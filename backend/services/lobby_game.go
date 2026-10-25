package services

import (
	"context"
	"fmt"
	"math"

	"islandBattles/backend/dal"
	"islandBattles/backend/models"
	"islandBattles/lobby/domain"
	"islandBattles/lobby/realtime"

	"github.com/jackc/pgx/v5/pgtype"
	"gorm.io/gorm"
)

// RunLobbyGame initializes one playable generation for the frozen roster and
// pushes it to every roster player. Repeated and concurrent starts reuse the
// committed state.
func RunLobbyGame(ctx context.Context, database *gorm.DB, publisher Publisher, snapshot domain.Snapshot) (models.BindingResult, error) {
	database = database.WithContext(ctx)
	binding, err := dal.BindLobbyGame(database, snapshot)
	if err != nil {
		return models.BindingResult{}, err
	}
	var events []realtime.Publication
	err = dal.WithLockedGame(database, binding.GameID, func(transaction *gorm.DB, game *models.Game) error {
		// Teams are saved with the first generation, so they mark an initialized game.
		if len(game.Teams) > 0 {
			return nil
		}
		if len(snapshot.StartRoster) == 0 {
			return fmt.Errorf("cannot initialize an empty roster")
		}
		teams, gameMap := prepareLobbyGame(binding, snapshot)
		players := make([]models.FrozenGamePlayer, 0, len(snapshot.StartRoster))
		for _, entry := range snapshot.StartRoster {
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
			players = append(players, models.FrozenGamePlayer{GameID: binding.GameID, PlayerID: entry.PlayerID, TeamColor: color})
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
		if err := dal.SaveGameTeams(transaction, game, teams); err != nil {
			return err
		}
		events, err = generationEvents(game, current, players)
		return err
	})
	if err != nil {
		return binding, err
	}
	if len(events) > 0 {
		// The game has committed. A failed push is logged and does not fail the start.
		_ = publisher.Publish(ctx, events...)
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
		for _, entry := range snapshot.StartRoster {
			if entry.TeamID != team.TeamID {
				continue
			}
			name := entry.PlayerID
			for _, member := range snapshot.Members {
				if member.PlayerID == entry.PlayerID {
					name = member.DisplayName
					break
				}
			}
			players = append(players, name)
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
