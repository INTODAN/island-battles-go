package services_test

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"islandBattles/backend/dal"
	"islandBattles/backend/models"
	"islandBattles/backend/services"
)

func TestReplayEscapesPlayerNames(t *testing.T) {
	// JSONB can return literal angle brackets even when the encoder escaped them.
	replay := []byte(`{"game_id":1,"teams":[{"name":"</script><script>alert(1)</script>"}],"frames":[],"outcome":""}`)
	if !json.Valid(replay) {
		t.Fatal("invalid fixture")
	}
	html, err := services.RenderGameReplay(replay)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(html, "<script>alert(1)</script>") {
		t.Fatal("unescaped user text in replay HTML")
	}
	if !strings.Contains(html, `\u003c/script\u003e`) {
		t.Fatal("expected escaped script terminator")
	}
}

func TestReplayFramesMatchLivePlayerStates(t *testing.T) {
	fixture := newTurnFixture(t, false)
	ctx := context.Background()
	states := []models.GenerationState{fixture.state}
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
	}
	game, err := dal.GetGameByID(fixture.database, fixture.state.GameID)
	if err != nil {
		t.Fatal(err)
	}
	var replay struct {
		Frames []json.RawMessage `json:"frames"`
	}
	if err := json.Unmarshal(game.Replay, &replay); err != nil {
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
		// Compare decoded JSON, because JSONB reorders keys.
		var expected, saved any
		if err := json.Unmarshal(live, &expected); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(replay.Frames[index], &saved); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(saved, expected) {
			t.Fatalf("frame %d = %s, live generation = %s", index, replay.Frames[index], live)
		}
	}
	if !groups || !clashes {
		t.Fatal("scenario never showed several soldier groups and a clash")
	}
}
