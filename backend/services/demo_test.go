package services_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"islandBattles/backend/dal"
	"islandBattles/backend/models"
	"islandBattles/backend/services"
	"islandBattles/lobby/domain"
)

// playLoggedTurns plays turns of the log fixture's game from its current
// generation. Every player logs a line naming themselves and the turn, from
// 1, with each completion. The fixture has one game, so its generation IDs
// count up from its first.
func playLoggedTurns(t *testing.T, fixture turnFixture, turns int) {
	t.Helper()
	ctx := context.Background()
	names := []string{"alice", "bob"}
	state, err := fixture.service.Current(ctx, fixture.players[0], fixture.state.LobbyID)
	if err != nil {
		t.Fatal(err)
	}
	for range turns {
		turn := state.Generation.ID - fixture.state.GenerationID
		var next models.GenerationState
		for index, player := range fixture.players {
			completed, err := fixture.service.Register(ctx, player, withLogs(batch(state, true), logLine(models.GenerationLogInfo, fmt.Sprintf("%s in turn %d", names[index], turn+1))))
			if err != nil {
				t.Fatal(err)
			}
			next = completed
		}
		state = next
	}
}

func saveDemo(t *testing.T, fixture turnFixture, player int) models.Demo {
	t.Helper()
	demo, err := fixture.service.Demo(context.Background(), fixture.players[player], fixture.state.LobbyID)
	if err != nil {
		t.Fatal(err)
	}
	return demo
}

// assertDemoMatchesTheGame checks that the demo holds the replay's frames in
// order, and for each the logs that the player reads for it now.
func assertDemoMatchesTheGame(t *testing.T, fixture turnFixture, player int, demo models.Demo) {
	t.Helper()
	ctx := context.Background()
	replay, err := fixture.service.Replay(ctx, fixture.players[player], fixture.state.LobbyID)
	if err != nil {
		t.Fatal(err)
	}
	if demo.Format != models.DemoFormat || demo.SchemaVersion != models.DemoSchemaVersion || demo.GameID != fixture.state.GameID ||
		!reflect.DeepEqual(demo.Teams, replay.Teams) || !reflect.DeepEqual(demo.Outcome, replay.Outcome) || !reflect.DeepEqual(demo.Frames, replay.Frames) {
		t.Fatalf("demo = %+v, replay = %+v", demo, replay)
	}
	if time.Since(demo.SavedAt) > time.Minute || demo.SavedAt.Location() != time.UTC {
		t.Fatalf("saved at %v", demo.SavedAt)
	}
	if len(demo.Logs) != len(demo.Frames) {
		t.Fatalf("%d log entries for %d frames", len(demo.Logs), len(demo.Frames))
	}
	for index, frame := range demo.Frames {
		read, err := fixture.service.Logs(ctx, fixture.players[player], fixture.state.LobbyID, frame.ID)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(demo.Logs[index], read) {
			t.Fatalf("frame %d logs = %+v, the logs read gives %+v", index, demo.Logs[index], read)
		}
	}
}

// demoMessages lists the messages of every line in the demo.
func demoMessages(demo models.Demo) []string {
	var messages []string
	for _, logs := range demo.Logs {
		for _, player := range logs.Players {
			for _, line := range player.Lines {
				messages = append(messages, line.Message)
			}
		}
	}
	return messages
}

func TestADemoHoldsEveryFrameWithTheLogsItsSaverMaySee(t *testing.T) {
	fixture := newLogFixture(t)
	playLoggedTurns(t, fixture, 2)

	// During the game, Alice's demo holds Blue's lines only, and Bob's Red's.
	alice := saveDemo(t, fixture, 0)
	assertDemoMatchesTheGame(t, fixture, 0, alice)
	if len(alice.Frames) != 3 || alice.Outcome != nil {
		t.Fatalf("running game's demo has %d frames and outcome %+v", len(alice.Frames), alice.Outcome)
	}
	if want := []string{"alice in turn 1", "alice in turn 2"}; !reflect.DeepEqual(demoMessages(alice), want) {
		t.Fatalf("Alice's demo lines = %q, want %q", demoMessages(alice), want)
	}
	for _, logs := range alice.Logs {
		if !reflect.DeepEqual(logs.HiddenTeamColors, []uint{2}) {
			t.Fatalf("Alice's demo hides %v", logs.HiddenTeamColors)
		}
	}
	// The open turn has no lines yet, and still lists Blue's players.
	if open := alice.Logs[2]; len(open.Players) != 1 || len(open.Players[0].Lines) != 0 {
		t.Fatalf("open turn = %+v", open)
	}
	bob := saveDemo(t, fixture, 1)
	assertDemoMatchesTheGame(t, fixture, 1, bob)
	if want := []string{"bob in turn 1", "bob in turn 2"}; !reflect.DeepEqual(demoMessages(bob), want) {
		t.Fatalf("Bob's demo lines = %q, want %q", demoMessages(bob), want)
	}

	// After the end, every demo holds every team's lines.
	game, err := dal.FindGame(fixture.database, fixture.state.GameID)
	if err != nil {
		t.Fatal(err)
	}
	winner := uint(1)
	if err := dal.SaveGameOutcome(fixture.database, game, models.GameOutcome{Reason: models.GameOutcomeLastTeamStanding, WinnerTeamColor: &winner}); err != nil {
		t.Fatal(err)
	}
	ended := saveDemo(t, fixture, 1)
	assertDemoMatchesTheGame(t, fixture, 1, ended)
	if ended.Outcome == nil || ended.Outcome.Summary != "Blue wins as the last team standing" {
		t.Fatalf("ended demo outcome = %+v", ended.Outcome)
	}
	if want := []string{"alice in turn 1", "bob in turn 1", "alice in turn 2", "bob in turn 2"}; !reflect.DeepEqual(demoMessages(ended), want) {
		t.Fatalf("ended demo lines = %q, want %q", demoMessages(ended), want)
	}
	for _, logs := range ended.Logs {
		if len(logs.HiddenTeamColors) != 0 || len(logs.Players) != 2 {
			t.Fatalf("ended demo logs = %+v", logs)
		}
	}
	// Players are named, never identified.
	raw, err := json.Marshal(ended)
	if err != nil {
		t.Fatal(err)
	}
	for _, player := range fixture.players {
		if strings.Contains(string(raw), player.PlayerID) {
			t.Fatalf("the demo names a player ID: %s", raw)
		}
	}
}

func TestADemoNeedsTheRoster(t *testing.T) {
	fixture := newLogFixture(t)
	ctx := context.Background()
	if _, err := fixture.service.Demo(ctx, domain.Principal{PlayerID: uuid.NewString()}, fixture.state.LobbyID); err == nil {
		t.Fatal("a player off the roster saved a demo")
	} else {
		assertTurnStatus(t, err, 404)
	}
	if _, err := fixture.service.Demo(ctx, fixture.players[0], uuid.NewString()); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("demo of an unknown lobby: %v", err)
	}
}

func TestADemoTakesAFixedNumberOfQueries(t *testing.T) {
	fixture := newLogFixture(t)
	queries := countQueries(t, fixture.database)
	demoQueries := func() int {
		before := *queries
		saveDemo(t, fixture, 0)
		return *queries - before
	}
	playLoggedTurns(t, fixture, 1)
	short := demoQueries()
	playLoggedTurns(t, fixture, 4)
	if long := demoQueries(); long != short {
		t.Fatalf("a demo of 6 generations took %d queries, of 2 generations %d", long, short)
	}
}

func TestWriteDemoStreamsWhatJSONEncodes(t *testing.T) {
	fixture := newLogFixture(t)
	playLoggedTurns(t, fixture, 2)
	demo := saveDemo(t, fixture, 0)
	demo.Teams[0].Name = "<Blue & co>"
	var streamed bytes.Buffer
	if err := services.WriteDemo(&streamed, demo); err != nil {
		t.Fatal(err)
	}
	if !json.Valid(streamed.Bytes()) {
		t.Fatalf("not JSON: %s", streamed.String())
	}
	// A file needs no HTML escapes.
	if !strings.Contains(streamed.String(), `"<Blue & co>"`) {
		t.Fatalf("names are escaped: %s", streamed.String())
	}
	marshaled, err := json.Marshal(demo)
	if err != nil {
		t.Fatal(err)
	}
	var fromStream, fromMarshal any
	if err := json.Unmarshal(streamed.Bytes(), &fromStream); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(marshaled, &fromMarshal); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(fromStream, fromMarshal) {
		t.Fatalf("streamed %s\nmarshaled %s", streamed.String(), marshaled)
	}
	var decoded models.Demo
	if err := json.Unmarshal(streamed.Bytes(), &decoded); err != nil {
		t.Fatal(err)
	}
	if !decoded.SavedAt.Equal(demo.SavedAt) {
		t.Fatalf("saved at %v, decoded %v", demo.SavedAt, decoded.SavedAt)
	}
	decoded.SavedAt = demo.SavedAt
	if !reflect.DeepEqual(decoded, demo) {
		t.Fatalf("decoded %+v, want %+v", decoded, demo)
	}
	if name := services.DemoFileName(demo); name != fmt.Sprintf("island-battles-game-%d-generation-3.json", demo.GameID) {
		t.Fatalf("file name %q", name)
	}
}

// countingWriter counts what is written to it and keeps none of it.
type countingWriter struct{ bytes int }

func (writer *countingWriter) Write(data []byte) (int, error) {
	writer.bytes += len(data)
	return len(data), nil
}

// The lobby page reads at most 64 MiB of a response (lobby/frontend/relay.mjs).
// A two-player game that runs to the generation cap, with both bots logging
// the most and the longest lines they may every turn, still fits. Packed
// with gzip, as browsers receive it, its varying bot-like lines take a
// fraction of that, which Lambda streams in seconds.
func TestAFullLengthTwoPlayerDemoWithFullLogsFitsTheLobbyPage(t *testing.T) {
	const relayLimit = 64 * 1024 * 1024
	upgrades := models.Upgrades{models.SpeedUpgrade: 3, models.ClashStopGenerationsUpgrade: 2}
	// Lines in a bot's wording, with numbers that change from line to line.
	random := rand.New(rand.NewPCG(80, 1))
	botLines := func() []models.GenerationLogLine {
		lines := make([]models.GenerationLogLine, models.MaxGenerationLogLines)
		for index := range lines {
			var message strings.Builder
			for message.Len() < models.MaxGenerationLogMessageLength {
				fmt.Fprintf(&message, "castle %d has %d soldiers; sending %d to castle %d at distance %.3f. ",
					random.IntN(20), random.IntN(500), random.IntN(100), random.IntN(20), random.Float64()*10)
			}
			lines[index] = models.GenerationLogLine{Level: models.GenerationLogWarning, Logger: "island_battles.player.strategy", Message: message.String()[:models.MaxGenerationLogMessageLength]}
		}
		return lines
	}
	demo := models.Demo{Format: models.DemoFormat, SchemaVersion: models.DemoSchemaVersion, SavedAt: time.Now().UTC(), GameID: 123456,
		Teams: []models.GameTeam{{Color: 1, Name: "Blue", Players: []string{"Alex"}}, {Color: 2, Name: "Red", Players: []string{"Sam"}}}}
	// A busy board of a large map: 20 castles, 20 groups on the move and 4
	// clashes in every generation.
	for generation := range services.MaxGenerations {
		frame := models.GenerationView{ID: uint(1_000_000 + generation)}
		for castle := range 20 {
			frame.Castles = append(frame.Castles, models.CastleView{ID: uint(castle), TeamColor: uint(castle % 3), Soldiers: 1234, X: -12.345678, Y: 6.789012, Upgrades: upgrades})
		}
		for group := range 20 {
			frame.SoldierGroups = append(frame.SoldierGroups, models.SoldierGroupView{ID: uint(10_000 + group), TeamColor: 1, Soldiers: 123, X: 1.23456789, Y: -2.3456789,
				OriginCastleID: 3, DestinationCastleID: 17, ClashRadius: 0.25, Upgrades: upgrades})
		}
		for clash := range 4 {
			participant := models.ClashParticipant{GroupID: uint(10_000 + clash), TeamColor: 2, X: 1.23456789, Y: -2.3456789, ClashRadius: 0.25, SoldiersBefore: 120, SoldiersAfter: 60}
			frame.Clashes = append(frame.Clashes, models.Clash{ID: uint(clash + 1), Time: 0.123456, Participants: []models.ClashParticipant{participant, participant}})
		}
		demo.Frames = append(demo.Frames, frame)
		demo.Logs = append(demo.Logs, models.GenerationLogs{GenerationID: frame.ID, HiddenTeamColors: []uint{}, Players: []models.PlayerGenerationLogs{
			{DisplayName: "Alex", TeamColor: 1, Lines: botLines()}, {DisplayName: "Sam", TeamColor: 2, Lines: botLines()},
		}})
	}
	var size countingWriter
	if err := services.WriteDemo(&size, demo); err != nil {
		t.Fatal(err)
	}
	var packed countingWriter
	if err := services.WriteCompressedDemo(&packed, demo); err != nil {
		t.Fatal(err)
	}
	t.Logf("a %d-generation two-player demo with full logs has %.1f MiB, and %.1f MiB packed", services.MaxGenerations,
		float64(size.bytes)/1024/1024, float64(packed.bytes)/1024/1024)
	if size.bytes > relayLimit {
		t.Fatalf("the demo has %d bytes, more than the lobby page's %d", size.bytes, relayLimit)
	}
	if packed.bytes > size.bytes/4 {
		t.Fatalf("the demo packs only from %d to %d bytes", size.bytes, packed.bytes)
	}
}
