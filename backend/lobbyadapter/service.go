// Package lobbyadapter binds a started generic lobby to Island Battles state.
// It is intentionally the only dependency direction from backend to lobby.
package lobbyadapter

import (
	"context"
	"errors"
	"sort"

	"islandBattles/backend/models"
	"islandBattles/lobby"

	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type TeamBinding struct {
	TeamID    string `json:"team_id"`
	TeamOrder int    `json:"team_order"`
	TeamColor uint   `json:"team_color"`
}

type BindingResult struct {
	GameID  uint          `json:"game_id"`
	LobbyID string        `json:"lobby_id"`
	Teams   []TeamBinding `json:"teams"`
}

// Service authorizes through the public lobby Application boundary and owns
// only the Island Battles game/link/mapping transaction.
type Service struct {
	database       *gorm.DB
	lobbies        lobby.Application
	insertBindings func(*gorm.DB, []TeamBindingRecord) error
}

func NewService(database *gorm.DB, lobbies lobby.Application) (*Service, error) {
	if database == nil || lobbies == nil {
		return nil, errors.New("lobby adapter: missing dependency")
	}
	return &Service{database: database, lobbies: lobbies, insertBindings: defaultInsertBindings}, nil
}

// Bind creates or replays an Island Battles game for one authorized, started
// island-battles lobby. Snapshot supplies the frozen roster and team order.
func (service *Service) Bind(ctx context.Context, principal lobby.Principal, scope lobby.Scope) (BindingResult, error) {
	if scope.GameKey != "island-battles" || lobby.ValidateUUID(scope.LobbyID) != nil {
		return BindingResult{}, lobby.ErrNotFound
	}
	snapshot, err := service.lobbies.Snapshot(ctx, principal, scope)
	if err != nil {
		return BindingResult{}, err
	}
	if snapshot.Lobby.GameKey != "island-battles" || snapshot.Lobby.ID != scope.LobbyID {
		return BindingResult{}, lobby.ErrNotFound
	}
	if snapshot.Lobby.Phase != lobby.PhaseStarted {
		return BindingResult{}, lobby.ErrPhaseConflict
	}
	if snapshot.Lobby.HostPlayerID == nil || *snapshot.Lobby.HostPlayerID != principal.PlayerID {
		return BindingResult{}, lobby.ErrNotHost
	}
	expected, err := expectedBindings(snapshot)
	if err != nil {
		return BindingResult{}, err
	}

	var result BindingResult
	err = service.database.WithContext(ctx).Transaction(func(transaction *gorm.DB) error {
		existing, err := loadGameByLobby(transaction, scope.LobbyID)
		if err != nil {
			return err
		}
		if existing != nil {
			result, err = loadBindingResult(transaction, *existing, scope.LobbyID, expected)
			return err
		}
		lobbyID := scope.LobbyID
		game := models.Game{LobbyID: &lobbyID, Generations: []models.Generation{}}
		if err := transaction.Create(&game).Error; err != nil {
			return err
		}
		records := make([]TeamBindingRecord, len(expected))
		for index, binding := range expected {
			records[index] = TeamBindingRecord{GameID: game.ID, TeamID: binding.TeamID, TeamOrder: binding.TeamOrder, TeamColor: binding.TeamColor}
		}
		if err := service.insertBindings(transaction, records); err != nil {
			return err
		}
		result = BindingResult{GameID: game.ID, LobbyID: scope.LobbyID, Teams: append([]TeamBinding(nil), expected...)}
		return nil
	})
	if err == nil {
		return result, nil
	}
	if !isLobbyLinkConflict(err) {
		return BindingResult{}, err
	}
	// A concurrent caller inserted the unique lobby link. Its transaction has
	// committed by the time this lookup succeeds; incomplete mappings stay an
	// error rather than an idempotent success.
	existing, lookupErr := loadGameByLobby(service.database.WithContext(ctx), scope.LobbyID)
	if lookupErr != nil {
		return BindingResult{}, lookupErr
	}
	if existing == nil {
		return BindingResult{}, err
	}
	return loadBindingResult(service.database.WithContext(ctx), *existing, scope.LobbyID, expected)
}

func expectedBindings(snapshot lobby.Snapshot) ([]TeamBinding, error) {
	if len(snapshot.StartRoster) == 0 || len(snapshot.Teams) == 0 {
		return nil, lobby.ErrPhaseConflict
	}
	bindings := make([]TeamBinding, len(snapshot.Teams))
	seenOrder := make(map[int]bool, len(snapshot.Teams))
	teamOrders := make(map[string]int, len(snapshot.Teams))
	for index, team := range snapshot.Teams {
		if lobby.ValidateUUID(team.ID) != nil || team.Order < 1 || seenOrder[team.Order] {
			return nil, lobby.ErrUnavailable
		}
		seenOrder[team.Order] = true
		teamOrders[team.ID] = team.Order
		bindings[index] = TeamBinding{TeamID: team.ID, TeamOrder: team.Order, TeamColor: uint(team.Order)}
	}
	for _, entry := range snapshot.StartRoster {
		if teamOrders[entry.TeamID] != entry.TeamOrder || entry.TeamOrder < 1 {
			return nil, lobby.ErrUnavailable
		}
	}
	sort.Slice(bindings, func(left, right int) bool { return bindings[left].TeamOrder < bindings[right].TeamOrder })
	for index, binding := range bindings {
		if binding.TeamOrder != index+1 {
			return nil, lobby.ErrUnavailable
		}
	}
	return bindings, nil
}

func loadGameByLobby(database *gorm.DB, lobbyID string) (*models.Game, error) {
	var games []models.Game
	if err := database.Where(&models.Game{LobbyID: &lobbyID}).Limit(1).Find(&games).Error; err != nil {
		return nil, err
	}
	if len(games) == 0 {
		return nil, nil
	}
	return &games[0], nil
}

func loadBindingResult(database *gorm.DB, game models.Game, lobbyID string, expected []TeamBinding) (BindingResult, error) {
	var records []TeamBindingRecord
	if err := database.Where(&TeamBindingRecord{GameID: game.ID}).Order(clause.OrderByColumn{Column: clause.Column{Name: "team_order"}}).Find(&records).Error; err != nil {
		return BindingResult{}, err
	}
	if len(records) != len(expected) {
		return BindingResult{}, lobby.ErrUnavailable
	}
	result := BindingResult{GameID: game.ID, LobbyID: lobbyID, Teams: make([]TeamBinding, len(records))}
	for index, record := range records {
		if record.TeamID != expected[index].TeamID || record.TeamOrder != expected[index].TeamOrder || record.TeamColor != expected[index].TeamColor {
			return BindingResult{}, lobby.ErrUnavailable
		}
		result.Teams[index] = TeamBinding{TeamID: record.TeamID, TeamOrder: record.TeamOrder, TeamColor: record.TeamColor}
	}
	return result, nil
}

func defaultInsertBindings(database *gorm.DB, records []TeamBindingRecord) error {
	return database.Create(&records).Error
}

func isLobbyLinkConflict(err error) bool {
	var postgresError *pgconn.PgError
	// backend/migrations owns this named unique index. Do not reinterpret an
	// unrelated unique/FK failure as an idempotent replay.
	return errors.As(err, &postgresError) && postgresError.Code == "23505" && postgresError.ConstraintName == "idx_games_lobby_id"
}
