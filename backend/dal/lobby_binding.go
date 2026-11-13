package dal

import (
	"errors"

	"github.com/INTODAN/mpl/domain"
	"islandBattles/backend/models"

	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// BindLobbyGame persists the game of the snapshot's lobby round, and its
// simulation colors, from the snapshot of a started lobby that the start hook
// has checked. Each round of a lobby gets its own game.
// models.NeutralTeamColor is reserved.
func BindLobbyGame(database *gorm.DB, snapshot domain.Snapshot) (models.BindingResult, error) {
	teams := make([]models.TeamBinding, len(snapshot.Teams))
	for index, team := range snapshot.Teams {
		teams[index] = models.TeamBinding{TeamID: team.ID, TeamOrder: team.Order, TeamColor: uint(team.Order)}
	}
	lobbyID, round := snapshot.Lobby.ID, snapshot.Lobby.Round
	return bindOnce(database, "idx_games_lobby_round", func(transaction *gorm.DB) (*models.BindingResult, error) {
		return loadLobbyBinding(transaction, lobbyID, round, teams)
	}, func(transaction *gorm.DB) (models.BindingResult, error) {
		game := models.Game{LobbyID: &lobbyID, LobbyRound: round, Generations: []models.Generation{}}
		if err := transaction.Create(&game).Error; err != nil {
			return models.BindingResult{}, err
		}
		records := make([]models.TeamBindingRecord, len(teams))
		for index, team := range teams {
			records[index] = models.TeamBindingRecord{GameID: game.ID, TeamID: team.TeamID, TeamOrder: team.TeamOrder, TeamColor: team.TeamColor}
		}
		if err := transaction.Create(&records).Error; err != nil {
			return models.BindingResult{}, err
		}
		return models.BindingResult{GameID: game.ID, LobbyID: lobbyID, Teams: teams}, nil
	})
}

func loadLobbyBinding(database *gorm.DB, lobbyID string, round int64, expected []models.TeamBinding) (*models.BindingResult, error) {
	var games []models.Game
	if err := database.Where(&models.Game{LobbyID: &lobbyID, LobbyRound: round}).Limit(1).Find(&games).Error; err != nil {
		return nil, err
	}
	if len(games) == 0 {
		return nil, nil
	}
	game := games[0]
	var records []models.TeamBindingRecord
	if err := database.Where(&models.TeamBindingRecord{GameID: game.ID}).Order(clause.OrderByColumn{Column: clause.Column{Name: "team_order"}}).Find(&records).Error; err != nil {
		return nil, err
	}
	if len(records) != len(expected) {
		return nil, domain.ErrUnavailable
	}
	result := &models.BindingResult{GameID: game.ID, LobbyID: lobbyID, Teams: make([]models.TeamBinding, len(records))}
	for index, record := range records {
		result.Teams[index] = models.TeamBinding{TeamID: record.TeamID, TeamOrder: record.TeamOrder, TeamColor: record.TeamColor}
		if result.Teams[index] != expected[index] {
			return nil, domain.ErrUnavailable
		}
	}
	return result, nil
}

// bindOnce runs a game's lookup and creation in one transaction. load must
// return nil only when no game exists, and reject incomplete persisted
// bindings. create must use the supplied transaction for all writes. Both
// callbacks must be free of external side effects. The database carries the
// request context.
//
// A concurrent creation is replayed only for the named unique lobby-link
// constraint. Unrelated database errors are returned without retrying.
func bindOnce[Result any](database *gorm.DB, constraint string, load func(*gorm.DB) (*Result, error), create func(*gorm.DB) (Result, error)) (Result, error) {
	var result Result
	err := database.Transaction(func(transaction *gorm.DB) error {
		existing, err := load(transaction)
		if err != nil {
			return err
		}
		if existing != nil {
			result = *existing
			return nil
		}
		result, err = create(transaction)
		return err
	})
	if err == nil {
		return result, nil
	}
	var zero Result
	var postgresError *pgconn.PgError
	if !errors.As(err, &postgresError) || postgresError.Code != "23505" || postgresError.ConstraintName != constraint {
		return zero, err
	}
	// The competing transaction has committed by the time its unique conflict
	// is reported. Load still validates that all of its game state is present.
	existing, lookupErr := load(database)
	if lookupErr != nil {
		return zero, lookupErr
	}
	if existing == nil {
		return zero, err
	}
	return *existing, nil
}
