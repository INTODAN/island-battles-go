package services_test

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"islandBattles/backend/dal"
	"islandBattles/backend/models"
	"islandBattles/backend/services"
	"islandBattles/lobby/domain"
	"islandBattles/lobby/realtime"
)

// lobbyWatchers stands in for the lobby's watcher rows: the players who may
// watch, and the ones the host kicked.
type lobbyWatchers struct {
	watching map[string]bool
	kicked   map[string]bool
}

func (watchers lobbyWatchers) AuthorizeWatch(_ context.Context, principal domain.Principal, scope domain.Scope) error {
	if scope.GameKey != "island-battles" {
		return domain.ErrNotFound
	}
	switch {
	case watchers.watching[principal.PlayerID]:
		return nil
	case watchers.kicked[principal.PlayerID]:
		return domain.ErrWatcherKicked
	}
	return domain.ErrNotFound
}

// failingWatchers is a lobby store that is down.
type failingWatchers struct{}

func (failingWatchers) AuthorizeWatch(context.Context, domain.Principal, domain.Scope) error {
	return domain.ErrUnavailable
}

// batchPublisher records each push as one batch, to check what a turn pushes
// together.
type batchPublisher struct {
	mutex   sync.Mutex
	batches [][]realtime.Publication
}

func (publisher *batchPublisher) Publish(_ context.Context, publications ...realtime.Publication) error {
	publisher.mutex.Lock()
	defer publisher.mutex.Unlock()
	publisher.batches = append(publisher.batches, publications)
	return nil
}

func TestWatchersGetEveryTurnInThePlayersPushAndNoPlayerFields(t *testing.T) {
	fixture := newLogFixture(t)
	ctx := context.Background()
	watcher, kicked, stranger := domain.Principal{PlayerID: uuid.NewString()}, domain.Principal{PlayerID: uuid.NewString()}, domain.Principal{PlayerID: uuid.NewString()}
	publisher := &batchPublisher{}
	service := &services.GenerationService{Database: fixture.database, Publisher: publisher,
		Watchers: lobbyWatchers{watching: map[string]bool{watcher.PlayerID: true}, kicked: map[string]bool{kicked.PlayerID: true}}}
	state := fixture.state

	watched, err := service.Watch(ctx, watcher, state.LobbyID)
	if err != nil {
		t.Fatal(err)
	}
	if watched.GameID != state.GameID || watched.GenerationID != state.GenerationID || watched.Channel != "watch:island-battles:"+state.LobbyID ||
		len(watched.Teams) != 2 || watched.Teams[0].Name != "Blue" || !reflect.DeepEqual(watched.Teams[0].Players, []string{"Alice", "Ada"}) ||
		!reflect.DeepEqual(watched.Generation, state.Generation) || watched.Outcome != nil {
		t.Fatalf("watch state = %+v", watched)
	}
	// The watcher's state carries none of a player's own fields.
	raw, err := json.Marshal(watched)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	keys := make([]string, 0, len(fields))
	for key := range fields {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	if !reflect.DeepEqual(keys, []string{"channel", "game_id", "generation", "generation_id", "lobby_id", "lobby_round", "teams", "turn_deadline_at"}) {
		t.Fatalf("watch state fields = %v", keys)
	}
	for _, player := range fixture.players {
		if strings.Contains(string(raw), player.PlayerID) {
			t.Fatalf("watch state names a player ID: %s", raw)
		}
	}
	// A kicked watcher is refused, and anyone else finds no game.
	if _, err := service.Watch(ctx, kicked, state.LobbyID); err == nil {
		t.Fatal("a kicked watcher read the game")
	} else {
		assertTurnStatus(t, err, 403)
	}
	if _, err := service.Watch(ctx, stranger, state.LobbyID); err == nil {
		t.Fatal("a stranger read the game")
	} else {
		assertTurnStatus(t, err, 404)
	}
	if _, err := service.WatchDemo(ctx, stranger, state.LobbyID); err == nil {
		t.Fatal("a stranger read the history")
	} else {
		assertTurnStatus(t, err, 404)
	}
	// A roster player reads what watchers read, also when the lobby kicked
	// their session as a watcher or its store fails.
	rosterKicked := &services.GenerationService{Database: fixture.database, Publisher: publisher,
		Watchers: lobbyWatchers{kicked: map[string]bool{fixture.players[1].PlayerID: true}}}
	for name, read := range map[string]func() error{
		"watch": func() error { _, err := rosterKicked.Watch(ctx, fixture.players[1], state.LobbyID); return err },
		"demo":  func() error { _, err := rosterKicked.WatchDemo(ctx, fixture.players[1], state.LobbyID); return err },
	} {
		if err := read(); err != nil {
			t.Fatalf("kicked roster player's %s read = %v", name, err)
		}
	}
	broken := &services.GenerationService{Database: fixture.database, Publisher: publisher, Watchers: failingWatchers{}}
	if _, err := broken.Watch(ctx, fixture.players[1], state.LobbyID); err != nil {
		t.Fatalf("roster player's watch read with the lobby store failing = %v", err)
	}
	if _, err := broken.Watch(ctx, stranger, state.LobbyID); err == nil || errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("a stranger's read with the lobby store failing = %v", err)
	}
	// A roster player reads what watchers read, also without a lobby.
	withoutLobby := &services.GenerationService{Database: fixture.database, Publisher: publisher}
	if read, err := withoutLobby.Watch(ctx, fixture.players[1], state.LobbyID); err != nil || read.GenerationID != state.GenerationID {
		t.Fatalf("roster player's watch read = %+v, %v", read, err)
	}

	// Watchers never count toward the turn barrier: the roster's three
	// completions play the turn, and its push carries the watch channel's
	// event with the players'.
	for _, player := range fixture.players {
		if _, err := service.Register(ctx, player, batch(state, true)); err != nil {
			t.Fatal(err)
		}
	}
	if len(publisher.batches) != 1 || len(publisher.batches[0]) != 4 {
		t.Fatalf("pushes = %+v", publisher.batches)
	}
	var watchEvent models.WatchGenerationEvent
	channels := map[string]bool{}
	for _, publication := range publisher.batches[0] {
		channels[publication.Channel] = true
		if strings.HasPrefix(publication.Channel, "watch:") {
			if err := json.Unmarshal(publication.Data, &watchEvent); err != nil {
				t.Fatal(err)
			}
		}
	}
	if len(channels) != 4 || !channels[watched.Channel] || watchEvent.Type != "game.next_generation" || watchEvent.SchemaVersion != 1 ||
		watchEvent.State.Channel != watched.Channel || watchEvent.State.GenerationID == state.GenerationID || len(watchEvent.State.Teams) != 2 {
		t.Fatalf("watch event = %+v on %v", watchEvent, channels)
	}
	read, err := service.Watch(ctx, watcher, state.LobbyID)
	readJSON, _ := json.Marshal(read)
	pushedJSON, _ := json.Marshal(watchEvent.State)
	if err != nil || string(readJSON) != string(pushedJSON) {
		t.Fatalf("read after the turn = %+v, %v; pushed %+v", read, err, watchEvent.State)
	}
}

func TestWatchersGetEveryTurnThatGoBotsPlay(t *testing.T) {
	game := startGoBotGame(t, lopsidedSettings, 3, 1)
	generations := game.generations(t)
	// Every generation, the first included, went to the watch channel, and
	// the last as game.ended.
	events := game.publisher.WatchEvents(t)
	if len(events) != int(generations) {
		t.Fatalf("pushed %d watch events for %d generations", len(events), generations)
	}
	for index, event := range events {
		last := index == len(events)-1
		if event.State.LobbyID != game.lobbyID || (event.Type == "game.ended") != last || (event.State.Outcome != nil) != last {
			t.Fatalf("watch event %d = %+v", index, event)
		}
		if index > 0 && event.State.GenerationID <= events[index-1].State.GenerationID {
			t.Fatalf("watch events out of order: %+v", events)
		}
	}
	// Each player event has its watch event in the same turn.
	if players := len(game.publisher.GenerationEvents(t)) + len(game.publisher.GameEndedEvents(t)); players != 2*len(events) {
		t.Fatalf("pushed %d player events for %d watch events", players, len(events))
	}
}

func TestWatchersSeeTheGameLogOnlyAfterTheEnd(t *testing.T) {
	fixture := newLogFixture(t)
	ctx := context.Background()
	watcher := domain.Principal{PlayerID: uuid.NewString()}
	service := &services.GenerationService{Database: fixture.database, Publisher: fixture.publisher, Watchers: lobbyWatchers{watching: map[string]bool{watcher.PlayerID: true}}}
	state := fixture.state
	alice, bob := fixture.players[0], fixture.players[1]
	if _, err := fixture.service.Register(ctx, alice, withLogs(batch(state, false), logLine(models.GenerationLogInfo, "Blue plans"))); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.service.Register(ctx, bob, withLogs(batch(state, false), logLine(models.GenerationLogInfo, "Red plans"))); err != nil {
		t.Fatal(err)
	}
	running, err := service.WatchDemo(ctx, watcher, state.LobbyID)
	if err != nil {
		t.Fatal(err)
	}
	if running.Outcome != nil || len(running.Frames) != 1 || len(running.Logs) != 1 || len(running.Logs[0].Players) != 0 || !reflect.DeepEqual(running.Logs[0].HiddenTeamColors, []uint{1, 2}) {
		t.Fatalf("running game's watch demo = %+v", running.Logs)
	}
	// Alice still sees her own team's lines in her own demo.
	if own, err := fixture.service.Demo(ctx, alice, state.LobbyID); err != nil || len(own.Logs[0].Players) != 2 || !reflect.DeepEqual(own.Logs[0].HiddenTeamColors, []uint{2}) {
		t.Fatalf("Alice's demo logs = %+v, %v", own.Logs, err)
	}

	game, err := dal.FindGame(fixture.database, state.GameID)
	if err != nil {
		t.Fatal(err)
	}
	if err := dal.SaveGameOutcome(fixture.database, game, models.GameOutcome{Reason: models.GameOutcomeGenerationCap}); err != nil {
		t.Fatal(err)
	}
	ended, err := service.WatchDemo(ctx, watcher, state.LobbyID)
	if err != nil {
		t.Fatal(err)
	}
	logs := ended.Logs[0]
	if ended.Outcome == nil || len(logs.HiddenTeamColors) != 0 || len(logs.Players) != 3 {
		t.Fatalf("ended game's watch demo = %+v", logs)
	}
	var messages []string
	for _, player := range logs.Players {
		for _, line := range player.Lines {
			messages = append(messages, line.Message)
		}
	}
	if !reflect.DeepEqual(messages, []string{"Blue plans", "Red plans"}) {
		t.Fatalf("lines after the end = %v", messages)
	}
	if watched, err := service.Watch(ctx, watcher, state.LobbyID); err != nil || watched.Outcome == nil || watched.TurnDeadlineAt != nil {
		t.Fatalf("ended watch state = %+v, %v", watched, err)
	}
}
