package services

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"

	"islandBattles/backend/dal"
	"islandBattles/backend/models"
	"islandBattles/lobby/domain"

	"github.com/jackc/pgx/v5/pgtype"
	"gorm.io/gorm"
)

// RunLobbyGame initializes one playable generation for the frozen roster.
// Repeated and concurrent starts reuse the committed state.
func RunLobbyGame(database *gorm.DB, snapshot domain.Snapshot) (models.BindingResult, error) {
	binding, err := dal.BindLobbyGame(database, snapshot)
	if err != nil {
		return models.BindingResult{}, err
	}
	err = dal.WithLockedGame(database, binding.GameID, func(transaction *gorm.DB, game *models.Game) error {
		// A saved replay also identifies old completed spectator games.
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
		replay.Frames = append(replay.Frames, playableReplayFrame(current))
		replay.Outcome = "Waiting for player turns"
		raw, err := json.Marshal(replay)
		if err != nil {
			return err
		}
		if err := dal.SaveGameReplay(transaction, game, raw); err != nil {
			return err
		}
		return queueGenerationNotifications(transaction, game, current, players)
	})
	return binding, err
}

type replayTeam struct {
	Color   uint     `json:"color"`
	Name    string   `json:"name"`
	Players []string `json:"players"`
}

type replayNode struct {
	ID          uint    `json:"id"`
	Color       uint    `json:"color"`
	Soldiers    uint    `json:"soldiers"`
	X           float64 `json:"x"`
	Y           float64 `json:"y"`
	ClashRadius float64 `json:"clash_radius,omitempty"`
}

type replayFrame struct {
	GenerationID uint         `json:"generation_id,omitempty"`
	Castles      []replayNode `json:"castles"`
	Groups       []replayNode `json:"groups"`
}

type gameReplay struct {
	GameID  uint          `json:"game_id"`
	Teams   []replayTeam  `json:"teams"`
	Frames  []replayFrame `json:"frames"`
	Outcome string        `json:"outcome"`
}

func prepareLobbyGame(binding models.BindingResult, snapshot domain.Snapshot) (gameReplay, models.Map) {
	replay := gameReplay{GameID: binding.GameID, Teams: []replayTeam{}, Frames: []replayFrame{}}
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
			CastleFields:  models.CastleFields{TeamColor: team.Color, PersistentCastleID: uint(index), Position: pgtype.Point{P: pgtype.Vec2{X: 2 * math.Cos(angle), Y: 2 * math.Sin(angle)}, Valid: true}},
			SoldierAmount: 30,
		})
	}
	return replay, gameMap
}

func playableReplayFrame(generation *models.Generation) replayFrame {
	frame := replayFrame{GenerationID: generation.ID, Castles: []replayNode{}, Groups: []replayNode{}}
	for _, castle := range generation.Map.Castles {
		frame.Castles = append(frame.Castles, replayNode{ID: castle.PersistentCastleID, Color: castle.TeamColor, Soldiers: castle.SoldierAmount, X: castle.Position.P.X, Y: castle.Position.P.Y})
	}
	for _, group := range generation.SoldierGroups {
		frame.Groups = append(frame.Groups, replayNode{ID: group.PersistentSoldierGroupID, Color: group.TeamColor, Soldiers: group.SoldierAmount, X: group.Position.P.X, Y: group.Position.P.Y, ClashRadius: group.EffectiveClashRadius()})
	}
	sort.Slice(frame.Castles, func(left, right int) bool { return frame.Castles[left].ID < frame.Castles[right].ID })
	sort.Slice(frame.Groups, func(left, right int) bool { return frame.Groups[left].ID < frame.Groups[right].ID })
	return frame
}

func appendPlayableReplay(database *gorm.DB, game *models.Game, generation *models.Generation) error {
	var replay gameReplay
	if err := json.Unmarshal(game.Replay, &replay); err != nil {
		return err
	}
	replay.Frames = append(replay.Frames, playableReplayFrame(generation))
	raw, err := json.Marshal(replay)
	if err != nil {
		return err
	}
	return dal.SaveGameReplay(database, game, raw)
}
