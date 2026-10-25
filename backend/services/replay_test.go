package services_test

import (
	"bytes"
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"gorm.io/gorm"
	"islandBattles/backend/dal"
	"islandBattles/backend/models"
	"islandBattles/backend/services"
)

func TestReplayEscapesPlayerNames(t *testing.T) {
	replay := services.GameReplay{GameID: 1, Teams: []models.GameTeam{{Name: "</script><script>alert(1)</script>", Players: []string{}}}, Frames: []models.GenerationView{}}
	html, err := services.RenderGameReplay(replay)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(html, "<script>alert(1)</script>") {
		t.Fatal("unescaped user text in replay HTML")
	}
	// The JSON encoder escapes the name's angle brackets, so it cannot close the script.
	if !strings.Contains(html, "\\u003c/script\\u003e\\u003cscript\\u003ealert(1)") {
		t.Fatal("expected escaped script terminator")
	}
}

// countQueries counts the SELECT statements the database runs, including
// each preload, until the test ends.
func countQueries(t *testing.T, database *gorm.DB) *int {
	t.Helper()
	queries := 0
	name := "test:count-queries"
	if err := database.Callback().Query().After("gorm:query").Register(name, func(*gorm.DB) { queries++ }); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Callback().Query().Remove(name) })
	return &queries
}

func TestReplayFramesMatchLivePlayerStates(t *testing.T) {
	fixture := newTurnFixture(t, false)
	ctx := context.Background()
	queries := countQueries(t, fixture.database)
	replayQueries := func() int {
		before := *queries
		if _, err := fixture.service.Replay(ctx, fixture.players[0], fixture.state.LobbyID); err != nil {
			t.Fatal(err)
		}
		return *queries - before
	}
	states := []models.GenerationState{fixture.state}
	var earlyQueries int
	for turn := 0; turn < 4; turn++ {
		state := states[len(states)-1]
		first, second := batch(state, true), batch(state, true)
		if turn == 0 {
			first.Commands = []models.PublicGenerationCommand{send(0, 1, 20), send(0, 1, 5)}
			second.Commands = []models.PublicGenerationCommand{send(1, 0, 10)}
		}
		if _, err := fixture.service.Register(ctx, fixture.players[0], first); err != nil {
			t.Fatal(err)
		}
		next, err := fixture.service.Register(ctx, fixture.players[1], second)
		if err != nil {
			t.Fatal(err)
		}
		states = append(states, next)
		if turn == 0 {
			earlyQueries = replayQueries()
		}
	}
	// Preloads are batched across generations, so a longer game takes no more queries.
	if lateQueries := replayQueries(); lateQueries != earlyQueries {
		t.Fatalf("replay of %d generations took %d queries, of 2 generations %d", len(states), lateQueries, earlyQueries)
	}
	replay, err := fixture.service.Replay(ctx, fixture.players[0], fixture.state.LobbyID)
	if err != nil {
		t.Fatal(err)
	}
	if len(replay.Frames) != len(states) {
		t.Fatalf("replay has %d frames for %d generations", len(replay.Frames), len(states))
	}
	groups, clashes := false, false
	for index, state := range states {
		groups = groups || len(state.Generation.SoldierGroups) > 1
		clashes = clashes || len(state.Generation.Clashes) > 0
		live, err := json.Marshal(state.Generation)
		if err != nil {
			t.Fatal(err)
		}
		frame, err := json.Marshal(replay.Frames[index])
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(frame, live) {
			t.Fatalf("frame %d = %s, live generation = %s", index, frame, live)
		}
	}
	if !groups || !clashes {
		t.Fatal("scenario never showed several soldier groups and a clash")
	}
	// The page reads the same keys it read from the saved replay blob.
	raw, err := json.Marshal(replay)
	if err != nil {
		t.Fatal(err)
	}
	var page map[string]json.RawMessage
	if err := json.Unmarshal(raw, &page); err != nil {
		t.Fatal(err)
	}
	if len(page) != 4 || page["game_id"] == nil || page["teams"] == nil || page["frames"] == nil || string(page["outcome"]) != `"Waiting for player turns"` {
		t.Fatalf("replay page data = %s", raw)
	}
	if len(replay.Teams) != 2 || replay.GameID != fixture.state.GameID {
		t.Fatalf("replay metadata = %+v", replay.Teams)
	}
}

func TestReplayShowsTheRecordedOutcome(t *testing.T) {
	fixture := newTurnFixture(t, false)
	ctx := context.Background()
	game, err := dal.FindLobbyGame(fixture.database, fixture.state.LobbyID)
	if err != nil {
		t.Fatal(err)
	}
	winner := game.Teams[1].Color
	for _, test := range []struct {
		outcome models.GameOutcome
		text    string
	}{
		{models.GameOutcome{Reason: models.GameOutcomeLastTeamStanding, WinnerTeamColor: &winner}, game.Teams[1].Name + " wins as the last team standing"},
		{models.GameOutcome{Reason: models.GameOutcomeAllEliminated}, "Draw: the last teams were eliminated in the same turn"},
		{models.GameOutcome{Reason: models.GameOutcomeGenerationCap, WinnerTeamColor: &winner}, game.Teams[1].Name + " wins with the most castles at the generation limit"},
		// An outcome without a winner is a draw, never "Nobody wins".
		{models.GameOutcome{Reason: models.GameOutcomeLastTeamStanding}, "Draw"},
		{models.GameOutcome{Reason: models.GameOutcomeGenerationCap}, "Draw: equal castle counts at the generation limit"},
	} {
		if err := dal.SaveGameOutcome(fixture.database, game, test.outcome); err != nil {
			t.Fatal(err)
		}
		replay, err := fixture.service.Replay(ctx, fixture.players[0], fixture.state.LobbyID)
		if err != nil {
			t.Fatal(err)
		}
		if replay.Outcome != test.text {
			t.Fatalf("outcome %+v shows %q, want %q", test.outcome, replay.Outcome, test.text)
		}
	}
	saved, err := dal.FindLobbyGame(fixture.database, fixture.state.LobbyID)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Outcome == nil || !reflect.DeepEqual(*saved.Outcome, models.GameOutcome{Reason: models.GameOutcomeGenerationCap}) {
		t.Fatalf("saved outcome = %+v", saved.Outcome)
	}
}
