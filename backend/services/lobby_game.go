package services

import (
	"context"
	"fmt"
	"math"
	"slices"

	"islandBattles/backend/dal"
	"islandBattles/backend/models"
	"islandBattles/backend/observability"
	"islandBattles/lobby/domain"
	"islandBattles/lobby/realtime"

	"github.com/jackc/pgx/v5/pgtype"
	"gorm.io/gorm"
)

// RunLobbyGame initializes one playable generation for the frozen roster and
// pushes it to every roster player. The generation starts on the map that the
// lobby's settings choose, or on the generated map without a choice. Repeated and concurrent starts reuse the
// committed state.
func RunLobbyGame(ctx context.Context, database *gorm.DB, publisher Publisher, snapshot domain.Snapshot) (models.BindingResult, error) {
	return runLobbyGame(ctx, database, publisher, snapshot, nil)
}

// runLobbyGame is RunLobbyGame, with the levels of the default bots that the
// backend plays for some roster players, by player ID. Their Go bots play the
// first generation before it is pushed. Nothing chooses the levels yet, so
// RunLobbyGame passes none: the players' bot choice (#111) and bot seats
// (#115) will.
func runLobbyGame(ctx context.Context, database *gorm.DB, publisher Publisher, snapshot domain.Snapshot, botLevels map[string]uint) (models.BindingResult, error) {
	database = database.WithContext(ctx)
	binding, err := dal.BindLobbyGame(database, snapshot)
	if err != nil {
		return models.BindingResult{}, err
	}
	var events []realtime.Publication
	// goBotsOnly is set when Go bots alone play the first turn, and have
	// completed it.
	goBotsOnly := false
	err = dal.WithLockedGame(database, binding.GameID, func(transaction *gorm.DB, game *models.Game) error {
		// Teams are saved with the first generation, so they mark an initialized game.
		if len(game.Teams) > 0 {
			return nil
		}
		if len(snapshot.StartRoster) == 0 {
			return fmt.Errorf("cannot initialize an empty roster")
		}
		teams, gameMap := prepareLobbyGame(binding, snapshot)
		// The lobby checked the settings at creation; a default map is loaded
		// from its template here.
		chosenMap, err := LobbyMap(transaction, snapshot.Lobby.GameSettings, len(snapshot.Teams))
		if err != nil {
			return fmt.Errorf("lobby map: %w", err)
		}
		if chosenMap != nil {
			gameMap = models.BuildMap(*chosenMap)
		}
		turnTimeout, err := LobbyTurnTimeout(snapshot.Lobby.GameSettings)
		if err != nil {
			return fmt.Errorf("lobby turn timeout: %w", err)
		}
		players := make([]models.FrozenGamePlayer, 0, len(snapshot.StartRoster))
		for index, entry := range snapshot.StartRoster {
			color := models.NeutralTeamColor
			for _, team := range binding.Teams {
				if team.TeamID == entry.TeamID {
					color = team.TeamColor
					break
				}
			}
			if color == models.NeutralTeamColor {
				return fmt.Errorf("frozen player has no team binding")
			}
			player := models.FrozenGamePlayer{GameID: binding.GameID, PlayerID: entry.PlayerID, TeamColor: color, DisplayName: startDisplayName(snapshot, index)}
			if level, found := botLevels[entry.PlayerID]; found {
				player.BotLevel = &level
			}
			players = append(players, player)
		}
		if err := dal.StoreFrozenPlayers(transaction, players); err != nil {
			return err
		}
		generation := models.Generation{GameID: binding.GameID, Map: gameMap}
		generationID, err := dal.CreateGeneration(transaction, &generation)
		if err != nil {
			return err
		}
		current, err := dal.GetGeneration(transaction, generationID)
		if err != nil {
			return err
		}
		if err := dal.SaveGameStart(transaction, game, teams, turnTimeout); err != nil {
			return err
		}
		inputs, err := playGoBots(ctx, transaction, game, current, players, nil)
		if err != nil {
			return err
		}
		goBotsOnly = slices.ContainsFunc(players, isGoBot) && turnComplete(game, current, players, inputs)
		memories, err := dal.LoadBotMemories(transaction, game.ID)
		if err != nil {
			return err
		}
		events, err = generationEvents(game, current, players, inputs, memories)
		return err
	})
	if err != nil {
		return binding, err
	}
	if len(events) > 0 {
		// The game has committed. A failed push is logged and does not fail the start.
		_ = publisher.Publish(ctx, events...)
	}
	if goBotsOnly {
		// The game has started, so a failure here only stops the Go bots
		// until the next game request.
		service := GenerationService{Database: database, Publisher: publisher}
		if _, err := service.playGoBotTurns(ctx, binding.GameID, goBotWriteBudget); err != nil {
			observability.RecordError(ctx, fmt.Errorf("play Go bot turns: %w", err))
		}
	}
	return binding, nil
}

// prepareLobbyGame copies the replay's teams out of the lobby, which the game
// never reads again, and places one castle per team.
func prepareLobbyGame(binding models.BindingResult, snapshot domain.Snapshot) ([]models.GameTeam, models.Map) {
	teams := []models.GameTeam{}
	gameMap := models.Map{MapFields: models.MapFields{MapName: "Lobby battle", PlayerAmount: uint(len(snapshot.StartRoster)), PlaneSizeX: 8, PlaneSizeY: 6}}
	for _, team := range binding.Teams {
		players := []string{}
		for index, entry := range snapshot.StartRoster {
			if entry.TeamID == team.TeamID {
				players = append(players, startDisplayName(snapshot, index))
			}
		}
		name := fmt.Sprintf("Team %d", team.TeamOrder)
		for _, lobbyTeam := range snapshot.Teams {
			if lobbyTeam.ID == team.TeamID {
				name = lobbyTeam.Name
				break
			}
		}
		teams = append(teams, models.GameTeam{Color: team.TeamColor, Name: name, Players: players})
	}
	for index, team := range teams {
		angle := math.Pi + 2*math.Pi*float64(index)/float64(len(teams))
		gameMap.Castles = append(gameMap.Castles, models.Castle{
			CastleFields:  models.CastleFields{TeamColor: team.Color, PersistentCastleID: uint(index), Position: pgtype.Point{P: pgtype.Vec2{X: 2 * math.Cos(angle), Y: 2 * math.Sin(angle)}, Valid: true}, Upgrades: models.Upgrades{}.Clone()},
			SoldierAmount: 30,
		})
	}
	return teams, gameMap
}

// startDisplayName is the lobby display name of the roster's player at index
// when the game started. The game copies it, because it never reads the lobby
// again. A player missing from the members is named by roster position, so
// a name never reveals a player ID.
func startDisplayName(snapshot domain.Snapshot, index int) string {
	playerID := snapshot.StartRoster[index].PlayerID
	for _, member := range snapshot.Members {
		if member.PlayerID == playerID {
			return member.DisplayName
		}
	}
	return fmt.Sprintf("Player %d", index+1)
}
