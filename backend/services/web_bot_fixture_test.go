package services

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"islandBattles/backend/models"
	"islandBattles/backend/simulation"
)

// TestWriteWebClientBotFixture plays the level 2 default bot (blue) against
// level 3 (red) on the tournament's "archipelago" map and writes a window of
// the game, where many groups clash next to castles, as the web client's
// fixture bot-clashes.json. It writes nothing unless WEB_CLIENT_FIXTURES_DIR
// names the directory:
//
//	WEB_CLIENT_FIXTURES_DIR=$PWD/clients/island-battles-web/test/fixtures go test ./backend/services -run TestWriteWebClientBotFixture
func TestWriteWebClientBotFixture(t *testing.T) {
	directory := os.Getenv("WEB_CLIENT_FIXTURES_DIR")
	if directory == "" {
		t.Skip("set WEB_CLIENT_FIXTURES_DIR to write the web client fixture")
	}
	var archipelago models.Map
	for _, candidate := range tournamentMaps() {
		if candidate.name == "archipelago" {
			archipelago = candidate.gameMap
		}
	}
	levels := []uint{2, 3}
	teams := []models.GameTeam{
		{Color: 1, Name: "Blue", Players: []string{"Level 2"}},
		{Color: 2, Name: "Red", Players: []string{"Level 3"}},
	}
	state := stateFromMap(archipelago)
	memories := make([]models.BotMemory, len(levels))
	frames := []models.GenerationView{fixtureView(1, state, nil)}
	for generations := 1; generations <= 48; generations++ {
		generation := generationFromState(uint(generations), state)
		inputs := []models.GenerationInput{}
		commands := []models.CommandFields{}
		for index, level := range levels {
			color := uint(index + 1)
			if !slices.Contains(standingTeams(generation, teams), color) {
				continue
			}
			turn := models.BotTurn{GameID: 1, TeamColor: color, Upgrades: models.UpgradeTable(), Memory: memories[index]}
			played, err := runGoBot(context.Background(), level, turn, generation, inputs)
			if err != nil {
				t.Fatalf("level %d failed generation %d: %v", level, generations, err)
			}
			memories[index] = played.memory
			commands = append(commands, played.commands...)
			inputs = append(inputs, models.GenerationInput{PlayerID: fmt.Sprint(index), Commands: marshalCommands(t, played.commands), Completed: true})
		}
		decoded, err := simulation.DecodeCommands(append(commands, simulation.EndOfTurnCommands()...))
		if err != nil {
			t.Fatal(err)
		}
		var events []simulation.Event
		if state, events, err = simulation.Simulate(state, decoded); err != nil {
			t.Fatalf("generation %d: %v", generations, err)
		}
		frames = append(frames, fixtureView(uint(generations+1), state, simulation.Clashes(events)))
	}
	// Generations 30 to 49: groups of both teams converge on the castles in the middle.
	frames = frames[29:49]
	logs := make([]models.GenerationLogs, len(frames))
	for index, frame := range frames {
		logs[index] = models.GenerationLogs{GenerationID: frame.ID, Players: []models.PlayerGenerationLogs{}, HiddenTeamColors: []uint{}}
	}
	demo := models.Demo{Format: "island-battles-demo", SchemaVersion: models.DemoSchemaVersion, SavedAt: time.Date(2026, 11, 12, 12, 0, 0, 0, time.UTC), GameID: 1, Teams: teams, Frames: frames, Logs: logs}
	data, err := json.Marshal(demo)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "bot-clashes.json"), append(data, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
}

// fixtureView is a generation as a demo file holds it.
func fixtureView(id uint, state simulation.GameState, clashes []models.Clash) models.GenerationView {
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
