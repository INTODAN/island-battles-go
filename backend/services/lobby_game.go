package services

import (
	"context"
	"encoding/json"
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
		// A saved replay means the game is already initialized.
		if len(game.Replay) > 0 {
			return nil
		}
		if len(snapshot.StartRoster) == 0 {
			return fmt.Errorf("cannot initialize an empty roster")
		}
		replay, gameMap := prepareLobbyGame(binding, snapshot)
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
		replay.Frames = append(replay.Frames, generationView(current))
		replay.Outcome = "Waiting for player turns"
		raw, err := json.Marshal(replay)
		if err != nil {
			return err
		}
		if err := dal.SaveGameReplay(transaction, game, raw); err != nil {
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

type replayTeam struct {
	Color   uint     `json:"color"`
	Name    string   `json:"name"`
	Players []string `json:"players"`
}

// gameReplay saves one frame per generation in the board view that players
// receive, so the replay page shows saved and live generations alike.
type gameReplay struct {
	GameID  uint                    `json:"game_id"`
	Teams   []replayTeam            `json:"teams"`
	Frames  []models.GenerationView `json:"frames"`
	Outcome string                  `json:"outcome"`
}

func prepareLobbyGame(binding models.BindingResult, snapshot domain.Snapshot) (gameReplay, models.Map) {
	replay := gameReplay{GameID: binding.GameID, Teams: []replayTeam{}, Frames: []models.GenerationView{}}
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
		// Empty configured teams do not take part in the battle.
		if len(players) == 0 {
			continue
		}
		name := fmt.Sprintf("Team %d", team.TeamOrder)
		for _, lobbyTeam := range snapshot.Teams {
			if lobbyTeam.ID == team.TeamID {
				name = lobbyTeam.Name
				break
			}
		}
		replay.Teams = append(replay.Teams, replayTeam{Color: team.TeamColor, Name: name, Players: players})
	}
	for index, team := range replay.Teams {
		angle := math.Pi + 2*math.Pi*float64(index)/float64(len(replay.Teams))
		gameMap.Castles = append(gameMap.Castles, models.Castle{
			CastleFields:  models.CastleFields{TeamColor: team.Color, PersistentCastleID: uint(index), Position: pgtype.Point{P: pgtype.Vec2{X: 2 * math.Cos(angle), Y: 2 * math.Sin(angle)}, Valid: true}, Upgrades: models.Upgrades{}.Clone()},
			SoldierAmount: 30,
		})
	}
	return replay, gameMap
}

func appendPlayableReplay(database *gorm.DB, game *models.Game, generation *models.Generation) error {
	var replay gameReplay
	if err := json.Unmarshal(game.Replay, &replay); err != nil {
		return err
	}
	replay.Frames = append(replay.Frames, generationView(generation))
	raw, err := json.Marshal(replay)
	if err != nil {
		return err
	}
	return dal.SaveGameReplay(database, game, raw)
}
