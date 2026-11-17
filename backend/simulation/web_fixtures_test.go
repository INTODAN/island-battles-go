package simulation_test

import (
	"encoding/json"
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"testing"
	"time"

	"islandBattles/backend/models"
	"islandBattles/backend/simulation"
)

// TestWriteWebClientFixtures plays seeded bot matches with the real simulation and writes
// them as demo files for the web client's fixtures (clients/island-battles-web/test/fixtures).
// It writes nothing unless WEB_CLIENT_FIXTURES_DIR names the directory:
//
//	WEB_CLIENT_FIXTURES_DIR=$PWD/clients/island-battles-web/test/fixtures go test ./backend/simulation -run TestWriteWebClientFixtures
func TestWriteWebClientFixtures(t *testing.T) {
	directory := os.Getenv("WEB_CLIENT_FIXTURES_DIR")
	if directory == "" {
		t.Skip("set WEB_CLIENT_FIXTURES_DIR to write the web client fixtures")
	}
	teamNames := []string{"Blue", "Red", "Green", "Purple", "Amber", "Teal"}

	// The generated map: one castle per team on a circle of radius 2 (services.prepareLobbyGame).
	generated := func(teamCount int) simulation.GameState {
		state := simulation.GameState{Castles: map[uint]*simulation.Castle{}, SoldierGroups: map[uint]*simulation.SoldierGroup{}}
		for index := range teamCount {
			angle := math.Pi + 2*math.Pi*float64(index)/float64(teamCount)
			state.Castles[uint(index)] = &simulation.Castle{ID: uint(index), TeamColor: uint(index + 1), Soldiers: 30, Position: point(2*math.Cos(angle), 2*math.Sin(angle)), Upgrades: models.Upgrades{}.Clone()}
		}
		return state
	}
	// A busier hand-placed map: Castle 3 (ID 2) lies on the line between Castles 1 and 2, and
	// Castles 6 and 10 are close enough to share an island.
	crossing := func() simulation.GameState {
		state := simulation.GameState{Castles: map[uint]*simulation.Castle{}, SoldierGroups: map[uint]*simulation.SoldierGroup{}}
		layout := []struct {
			team uint
			x, y float64
		}{{1, -4, 0}, {2, 4, 0}, {0, 0, 0}, {1, -4, 2.5}, {2, 4, -2.5}, {0, -3.5, -2.5}, {0, 2, 2.5}, {0, 0, -2.8}, {0, -1.5, 2.5}, {0, -2.6, -2.9}}
		for index, entry := range layout {
			soldiers := uint(10)
			if entry.team != 0 {
				soldiers = 30
			}
			state.Castles[uint(index)] = &simulation.Castle{ID: uint(index), TeamColor: entry.team, Soldiers: soldiers, Position: point(entry.x, entry.y), Upgrades: models.Upgrades{}.Clone()}
		}
		return state
	}

	write := func(name string, teamCount int, state simulation.GameState, turns int, seed int64, window [2]int) {
		random := rand.New(rand.NewSource(seed))
		endOfTurn, err := simulation.DecodeCommands(simulation.EndOfTurnCommands())
		if err != nil {
			t.Fatal(err)
		}
		frames := []models.GenerationView{view(1, state, nil)}
		for turn := 2; turn <= turns+1; turn++ {
			var commands []simulation.Command
			for _, castle := range state.SortedCastles() {
				if castle.TeamColor == 0 || castle.Soldiers < 8 {
					continue
				}
				switch roll := random.Float64(); {
				case roll < 0.55:
					destination := uint(random.Intn(len(state.Castles)))
					if destination != castle.ID {
						commands = append(commands, simulation.SendSoldierGroupParams{OriginPersistentCastleID: castle.ID, DestinationPersistentCastleID: destination, SoldierAmount: castle.Soldiers / 2})
					}
				case roll < 0.75:
					upgradeType := models.UpgradeTypes()[random.Intn(len(models.UpgradeTypes()))]
					commands = append(commands, simulation.UpgradeCastleParams{PersistentCastleID: castle.ID, UpgradeType: upgradeType})
				}
			}
			next, events, err := simulation.Simulate(state, append(applicableCommands(state, commands), endOfTurn...))
			if err != nil {
				t.Fatal(err)
			}
			state = next
			frames = append(frames, view(uint(turn), state, simulation.Clashes(events)))
		}
		if window[1] > 0 {
			frames = frames[window[0]:window[1]]
		}
		teams := make([]models.GameTeam, teamCount)
		logs := make([]models.GenerationLogs, len(frames))
		for index := range teams {
			teams[index] = models.GameTeam{Color: uint(index + 1), Name: teamNames[index], Players: []string{"Bot " + teamNames[index]}}
		}
		for index, frame := range frames {
			logs[index] = models.GenerationLogs{GenerationID: frame.ID, Players: []models.PlayerGenerationLogs{}, HiddenTeamColors: []uint{}}
		}
		demo := models.Demo{Format: "island-battles-demo", SchemaVersion: models.DemoSchemaVersion, SavedAt: time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC), GameID: 1, Teams: teams, Frames: frames, Logs: logs}
		data, err := json.Marshal(demo)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(directory, name), append(data, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	write("generated-map.json", 2, generated(2), 6, 1, [2]int{0, 7})
	write("six-teams.json", 6, generated(6), 14, 2, [2]int{0, 15})
	write("busy-turn.json", 2, crossing(), 40, 7, [2]int{16, 28})
}

// applicableCommands keeps the commands that still apply, one after another, to the state.
// The bot may choose two upgrades that together overspend a castle.
func applicableCommands(state simulation.GameState, commands []simulation.Command) []simulation.Command {
	kept := []simulation.Command{}
	for _, command := range commands {
		if _, _, err := simulation.Simulate(state, append(append([]simulation.Command{}, kept...), command)); err == nil {
			kept = append(kept, command)
		}
	}
	return kept
}

func view(id uint, state simulation.GameState, clashes []models.Clash) models.GenerationView {
	generation := models.GenerationView{ID: id, Castles: []models.CastleView{}, SoldierGroups: []models.SoldierGroupView{}, Clashes: []models.Clash{}}
	generation.Clashes = append(generation.Clashes, clashes...)
	for _, castle := range state.SortedCastles() {
		generation.Castles = append(generation.Castles, models.CastleView{ID: castle.ID, TeamColor: castle.TeamColor, Soldiers: castle.Soldiers, X: castle.Position.P.X, Y: castle.Position.P.Y, Upgrades: castle.Upgrades.Clone()})
	}
	for _, group := range state.SortedSoldierGroups() {
		generation.SoldierGroups = append(generation.SoldierGroups, models.SoldierGroupView{ID: group.ID, TeamColor: group.TeamColor, Soldiers: group.Soldiers, X: group.Position.P.X, Y: group.Position.P.Y, OriginCastleID: group.OriginCastleID, DestinationCastleID: group.DestinationCastleID, ClashRadius: group.EffectiveClashRadius(), Upgrades: group.Upgrades.Clone()})
	}
	return generation
}
