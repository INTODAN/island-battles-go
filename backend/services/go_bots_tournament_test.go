package services

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"

	"islandBattles/backend/bots"
	"islandBattles/backend/models"
	"islandBattles/backend/simulation"
	"islandBattles/lobby/domain"

	"github.com/jackc/pgx/v5/pgtype"
	"gorm.io/gorm"
)

// tournamentMap is a map the default bots play on, for two teams.
type tournamentMap struct {
	name    string
	gameMap models.Map
}

// generatedTournamentMap is the map a lobby without a map choice starts on,
// for two teams.
func generatedTournamentMap() tournamentMap {
	binding := models.BindingResult{Teams: []models.TeamBinding{{TeamID: "blue", TeamOrder: 1, TeamColor: 1}, {TeamID: "red", TeamOrder: 2, TeamColor: 2}}}
	snapshot := domain.Snapshot{Teams: []domain.Team{{ID: "blue", Order: 1, Name: "Blue"}, {ID: "red", Order: 2, Name: "Red"}},
		StartRoster: []domain.StartRosterEntry{{PlayerID: "a", TeamID: "blue"}, {PlayerID: "b", TeamID: "red"}}}
	_, gameMap := prepareLobbyGame(binding, snapshot)
	return tournamentMap{name: "generated", gameMap: gameMap}
}

// neutralTournamentMap mirrors castles, each at x and y with soldiers, around
// the centre for two teams, whose castles are at (-4, 0) and (4, 0).
func neutralTournamentMap(name string, neutrals ...[3]float64) tournamentMap {
	thirty := uint(30)
	template := models.MapTemplateView{MapFields: models.MapFields{MapName: name, PlayerAmount: 2, PlaneSizeX: 10, PlaneSizeY: 8},
		Castles: []models.CastleTemplateView{{TeamColor: 1, X: -4, StartingSoldierAmount: &thirty}, {TeamColor: 2, X: 4, StartingSoldierAmount: &thirty}}}
	for _, neutral := range neutrals {
		soldiers := uint(neutral[2])
		template.Castles = append(template.Castles,
			models.CastleTemplateView{X: -neutral[0], Y: neutral[1], StartingSoldierAmount: &soldiers},
			models.CastleTemplateView{X: neutral[0], Y: -neutral[1], StartingSoldierAmount: &soldiers})
	}
	return tournamentMap{name: name, gameMap: models.BuildMap(template)}
}

func tournamentMaps() []tournamentMap {
	return []tournamentMap{
		generatedTournamentMap(),
		neutralTournamentMap("islands", [3]float64{2.5, 2, 10}, [3]float64{1, 1.5, 15}, [3]float64{2.5, -2.5, 20}),
		neutralTournamentMap("strait", [3]float64{1.5, 0.5, 5}, [3]float64{3, 3, 25}, [3]float64{0.5, 3, 15}),
		neutralTournamentMap("archipelago", [3]float64{3, 1.5, 8}, [3]float64{3.5, -2, 12}, [3]float64{1.5, 2.5, 18}, [3]float64{0, 1, 30}, [3]float64{2, -0.5, 6}),
	}
}

// generationFromState is the stored generation of a board, as the bots and
// the command checks see it.
func generationFromState(id uint, state simulation.GameState) *models.Generation {
	generation := &models.Generation{Model: gorm.Model{ID: id}}
	castles := map[uint]models.Castle{}
	for _, castle := range state.SortedCastles() {
		stored := models.Castle{CastleFields: models.CastleFields{TeamColor: castle.TeamColor, Position: castle.Position, PersistentCastleID: castle.ID, Upgrades: castle.Upgrades.Clone()}, SoldierAmount: castle.Soldiers}
		castles[castle.ID] = stored
		generation.Map.Castles = append(generation.Map.Castles, stored)
	}
	for _, group := range state.SortedSoldierGroups() {
		generation.SoldierGroups = append(generation.SoldierGroups, models.SoldierGroup{
			SoldierGroupFields: models.SoldierGroupFields{SoldierAmount: group.Soldiers}, PersistentSoldierGroupID: group.ID, TeamColor: group.TeamColor,
			OriginCastle: castles[group.OriginCastleID], DestinationCastle: castles[group.DestinationCastleID],
			MovementPauseGenerations: group.MovementPauseGenerations, ClashRadius: group.ClashRadius, Position: group.Position, Upgrades: group.Upgrades.Clone(),
		})
	}
	return generation
}

func stateFromMap(gameMap models.Map) simulation.GameState {
	state := simulation.GameState{Map: gameMap.MapFields, Castles: map[uint]*simulation.Castle{}, SoldierGroups: map[uint]*simulation.SoldierGroup{}}
	for _, castle := range gameMap.Castles {
		state.Castles[castle.PersistentCastleID] = &simulation.Castle{ID: castle.PersistentCastleID, TeamColor: castle.TeamColor, Soldiers: castle.SoldierAmount,
			Position: pgtype.Point{P: castle.Position.P, Valid: true}, Upgrades: castle.Upgrades.Clone()}
	}
	return state
}

// playTournamentGame plays a game between Go bots, levels[i] for team colour
// i+1, as the service plays their turns, to its outcome. Every turn of every
// bot must pass the command checks.
func playTournamentGame(t *testing.T, gameID uint, gameMap models.Map, levels []uint) (*models.GameOutcome, int) {
	t.Helper()
	teams := []models.GameTeam{}
	for index := range levels {
		teams = append(teams, models.GameTeam{Color: uint(index + 1), Name: fmt.Sprintf("Level %d", levels[index])})
	}
	state := stateFromMap(gameMap)
	memories := make([]models.BotMemory, len(levels))
	for generations := 1; ; generations++ {
		generation := generationFromState(uint(generations), state)
		inputs := []models.GenerationInput{}
		commands := []models.CommandFields{}
		for index, level := range levels {
			color := uint(index + 1)
			if !slices.Contains(standingTeams(generation, teams), color) {
				continue
			}
			turn := models.BotTurn{GameID: gameID, Generation: GenerationView(generation), TeamColor: color, Memory: memories[index]}
			played, err := runGoBot(context.Background(), level, turn, generation, inputs)
			if err != nil {
				t.Fatalf("level %d failed generation %d: %v", level, generations, err)
			}
			if len(played.logs) == 0 || len(played.logs) > 2 {
				t.Fatalf("level %d wrote %d log lines in generation %d, want 1 or 2", level, len(played.logs), generations)
			}
			memories[index] = played.memory
			commands = append(commands, played.commands...)
			inputs = append(inputs, models.GenerationInput{PlayerID: fmt.Sprint(index), Commands: marshalCommands(t, played.commands), Completed: true})
		}
		decoded, err := simulation.DecodeCommands(append(commands, simulation.EndOfTurnCommands()...))
		if err != nil {
			t.Fatal(err)
		}
		if state, _, err = simulation.Simulate(state, decoded); err != nil {
			t.Fatalf("generation %d: %v", generations, err)
		}
		if outcome := simulation.Outcome(state, teams, generations+1 >= MaxGenerations); outcome != nil {
			return outcome, generations + 1
		}
	}
}

// TestGoBotTournament plays every pair of default levels, on each side of
// every tournament map, to an outcome. Set PRINT_BOT_TOURNAMENT to print
// every game and the table.
func TestGoBotTournament(t *testing.T) {
	type record struct{ wins, draws, losses int }
	levels := []uint{}
	for _, bot := range bots.Defaults() {
		levels = append(levels, bot.Level)
	}
	results := map[[2]uint]*record{}
	games := []string{}
	gameID := uint(1)
	for _, tournamentMap := range tournamentMaps() {
		for _, first := range levels {
			for _, second := range levels {
				outcome, generations := playTournamentGame(t, gameID, tournamentMap.gameMap, []uint{first, second})
				gameID++
				winner := "draw"
				if outcome.WinnerTeamColor != nil {
					winner = fmt.Sprintf("level %d", []uint{first, second}[*outcome.WinnerTeamColor-1])
				}
				games = append(games, fmt.Sprintf("%-11s level %d (blue) vs level %d (red): %s, %s after %d generations", tournamentMap.name, first, second, winner, outcome.Reason, generations))
				for side, level := range []uint{first, second} {
					opponent := []uint{first, second}[1-side]
					pair := [2]uint{level, opponent}
					if results[pair] == nil {
						results[pair] = &record{}
					}
					switch {
					case first == second && side == 1:
					case outcome.WinnerTeamColor == nil:
						results[pair].draws++
					case *outcome.WinnerTeamColor == uint(side+1):
						results[pair].wins++
					default:
						results[pair].losses++
					}
				}
			}
		}
	}
	table := []string{"| Level | Against | Won | Drawn | Lost |", "|---|---|---|---|---|"}
	for _, level := range levels {
		for _, opponent := range levels {
			result := results[[2]uint{level, opponent}]
			table = append(table, fmt.Sprintf("| %d | %d | %d | %d | %d |", level, opponent, result.wins, result.draws, result.losses))
		}
	}
	if _, print := os.LookupEnv("PRINT_BOT_TOURNAMENT"); print {
		t.Log("\n" + strings.Join(games, "\n") + "\n\n" + strings.Join(table, "\n"))
	}
	for _, pair := range [][2]uint{{3, 1}, {3, 2}, {2, 1}} {
		result := results[pair]
		if result.wins*2 <= result.wins+result.draws+result.losses {
			t.Errorf("level %d won %d of %d games against level %d, want most", pair[0], result.wins, result.wins+result.draws+result.losses, pair[1])
		}
	}
}

func marshalCommands(t *testing.T, commands []models.CommandFields) []byte {
	t.Helper()
	raw, err := json.Marshal(commands)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
