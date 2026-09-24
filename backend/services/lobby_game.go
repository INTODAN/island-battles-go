package services

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"

	"islandBattles/backend/dal"
	"islandBattles/backend/dal/commandHandlers"
	"islandBattles/backend/models"
	"islandBattles/lobby/domain"

	"github.com/jackc/pgx/v5/pgtype"
	"gorm.io/gorm"
)

const spectatorTurns = 60

// RunLobbyGame creates one automatic, bounded simulation for the frozen roster.
// Replays and concurrent starts reuse its saved result, including after restart.
func RunLobbyGame(database *gorm.DB, snapshot domain.Snapshot) (models.BindingResult, error) {
	binding, err := dal.BindLobbyGame(database, snapshot)
	if err != nil {
		return models.BindingResult{}, err
	}
	err = dal.StoreGameReplayOnce(database, binding.GameID, func(transaction *gorm.DB) ([]byte, error) {
		return simulateLobbyGame(transaction, binding, snapshot)
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
	Castles []replayNode `json:"castles"`
	Groups  []replayNode `json:"groups"`
}

type gameReplay struct {
	GameID  uint          `json:"game_id"`
	Teams   []replayTeam  `json:"teams"`
	Frames  []replayFrame `json:"frames"`
	Outcome string        `json:"outcome"`
}

func simulateLobbyGame(database *gorm.DB, binding models.BindingResult, snapshot domain.Snapshot) ([]byte, error) {
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
			CastleFields:  models.CastleFields{TeamColor: team.Color, LogicalCastleID: uint(index), Position: pgtype.Point{P: pgtype.Vec2{X: 2 * math.Cos(angle), Y: 2 * math.Sin(angle)}, Valid: true}},
			SoldierAmount: 30,
		})
	}
	generation := models.Generation{GameID: binding.GameID, Map: gameMap}
	generationID, err := dal.CreateGeneration(database, &generation)
	if err != nil {
		return nil, err
	}
	for turn := 0; turn <= spectatorTurns; turn++ {
		current, err := dal.GetGeneration(database, generationID)
		if err != nil {
			return nil, err
		}
		// Stable ordering also breaks equal-distance targeting ties consistently.
		sort.Slice(current.Map.Castles, func(left, right int) bool {
			return current.Map.Castles[left].LogicalCastleID < current.Map.Castles[right].LogicalCastleID
		})
		frame := replayFrame{Castles: []replayNode{}, Groups: []replayNode{}}
		activeTeams := map[uint]bool{}
		for _, castle := range current.Map.Castles {
			frame.Castles = append(frame.Castles, replayNode{ID: castle.LogicalCastleID, Color: castle.TeamColor, Soldiers: castle.SoldierAmount, X: castle.Position.P.X, Y: castle.Position.P.Y})
			activeTeams[castle.TeamColor] = true
		}
		for _, group := range current.SoldierGroups {
			frame.Groups = append(frame.Groups, replayNode{ID: group.LogicalSoldierGroupID, Color: group.TeamColor, Soldiers: group.SoldierAmount, X: group.Position.P.X, Y: group.Position.P.Y, ClashRadius: group.EffectiveClashRadius()})
			if group.SoldierAmount > 0 {
				activeTeams[group.TeamColor] = true
			}
		}
		replay.Frames = append(replay.Frames, frame)
		if len(activeTeams) == 1 {
			for _, team := range replay.Teams {
				if activeTeams[team.Color] {
					replay.Outcome = team.Name + " wins"
				}
			}
			break
		}
		if turn == spectatorTurns {
			replay.Outcome = "Turn limit reached — no winner"
			break
		}
		commands := []models.CommandFields{}
		for _, castle := range current.Map.Castles {
			// Stagger the teams' attacks. Players never submit these commands.
			if turn%6 != int(castle.TeamColor-1)%2*3 || castle.SoldierAmount < 4 {
				continue
			}
			var target *models.Castle
			nearest := math.Inf(1)
			for index := range current.Map.Castles {
				candidate := &current.Map.Castles[index]
				if candidate.TeamColor == castle.TeamColor {
					continue
				}
				distance := math.Hypot(candidate.Position.P.X-castle.Position.P.X, candidate.Position.P.Y-castle.Position.P.Y)
				if distance < nearest {
					target, nearest = candidate, distance
				}
			}
			if target == nil {
				continue
			}
			parameters, err := json.Marshal(commandHandlers.SendSoldierGroupParams{OriginLogicalCastleID: castle.LogicalCastleID, DestinationLogicalCastleID: target.LogicalCastleID, SoldierAmount: castle.SoldierAmount / 2})
			if err != nil {
				return nil, err
			}
			commands = append(commands, models.CommandFields{CommandType: models.SendSoldierGroup, Parameters: parameters})
		}
		generationID, err = MigrateGeneration(database, binding.GameID, commands)
		if err != nil {
			return nil, err
		}
	}
	return json.Marshal(replay)
}
