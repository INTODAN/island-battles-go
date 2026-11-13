package islandbattles

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

const (
	testLobbyID  = "8c1f0d1e-5f7a-4c7e-9a52-3b0b1d2e4f60"
	testPlayerID = "0b6a3c8e-1d2f-4e5a-8b7c-9d0e1f2a3b4c"
)

// fakeServer is the game server's player API, with one player's state.
type fakeServer struct {
	t      *testing.T
	server *httptest.Server
	mutex  sync.Mutex
	state  map[string]any
	// batches are the bodies of POST /generation/commands, raw.
	batches   [][]byte
	deadlines int
	reads     int
	// answer answers a batch; nil completes the turn.
	answer func(writer http.ResponseWriter, request *http.Request, body []byte) bool
	// deadline answers POST /deadline; nil answers the state.
	deadline func() map[string]any
	// failReads answers this many generation reads with 503.
	failReads int
	// issued counts the player tokens the lobby issued; valid are those the
	// game accepts, and refuseTokens makes it refuse every one.
	issued       int
	valid        map[string]bool
	refuseTokens bool
}

func newFakeServer(t *testing.T) *fakeServer {
	fake := &fakeServer{t: t, valid: map[string]bool{}}
	fake.state = fake.stateOf(1, false, nil)
	fake.server = httptest.NewServer(http.HandlerFunc(fake.handle))
	t.Cleanup(fake.server.Close)
	return fake
}

// stateOf is a GenerationState of the fixture's board.
func (fake *fakeServer) stateOf(generationID uint, submitted bool, outcome map[string]any) map[string]any {
	turn := FixtureTurn()
	turn.Generation.ID = generationID
	state := map[string]any{
		"game_id": 7, "lobby_id": testLobbyID, "lobby_round": 1, "generation_id": generationID, "player_id": testPlayerID,
		"team_color": 1, "channel": "player:" + testPlayerID, "submitted": submitted, "generation": turn.Generation,
		"log_lines_stored": 0, "memory": nil, "bot": map[string]any{"kind": "localhost"}, "turn_deadline_at": nil,
	}
	if outcome != nil {
		state["outcome"] = outcome
	}
	return state
}

func (fake *fakeServer) setState(state map[string]any) {
	fake.mutex.Lock()
	defer fake.mutex.Unlock()
	fake.state = state
}

func (fake *fakeServer) handle(writer http.ResponseWriter, request *http.Request) {
	authorization := request.Header.Get("Authorization")
	body, _ := io.ReadAll(request.Body)
	fake.mutex.Lock()
	// The lobby takes the guest credential; the game, the player tokens the
	// lobby issued.
	authorized := authorization == "Bearer good"
	if !strings.HasPrefix(request.URL.Path, "/lobby/") {
		authorized = !fake.refuseTokens && fake.valid[strings.TrimPrefix(authorization, "Bearer ")]
	}
	if !authorized {
		fake.mutex.Unlock()
		writeJSON(writer, http.StatusUnauthorized, map[string]any{"title": "Unauthorized", "status": 401, "detail": "bad credential", "code": "unauthenticated"})
		return
	}
	if request.URL.Path == "/lobby/v1/player-tokens" {
		if string(body) != `{"game_key":"island-battles"}` {
			fake.t.Errorf("player token request = %s", body)
		}
		fake.issued++
		token := fmt.Sprintf("player-token-%d", fake.issued)
		fake.valid[token] = true
		fake.mutex.Unlock()
		writeJSON(writer, 200, map[string]any{"$schema": "http://lobby.test/schemas/PlayerToken.json", "token": token, "expires_at": time.Now().Add(15 * time.Minute)})
		return
	}
	if request.Method == http.MethodGet && strings.HasSuffix(request.URL.Path, "/generation") && fake.failReads > 0 {
		fake.failReads--
		fake.mutex.Unlock()
		writeJSON(writer, 503, map[string]any{"detail": "injected"})
		return
	}
	state := fake.state
	switch {
	case request.Method == http.MethodGet && strings.HasSuffix(request.URL.Path, "/generation"):
		fake.reads++
	case request.URL.Path == "/generation/commands":
		fake.batches = append(fake.batches, body)
	case strings.HasSuffix(request.URL.Path, "/deadline"):
		fake.deadlines++
	}
	answer, deadline := fake.answer, fake.deadline
	fake.mutex.Unlock()
	switch request.URL.Path {
	case "/lobby/v1/connection-token":
		writeJSON(writer, 200, map[string]any{"token": "t", "expires_at": time.Now().Add(time.Hour), "websocket_url": "ws://127.0.0.1:1/connection/websocket"})
	case "/game/upgrades":
		writeJSON(writer, 200, map[string]any{"upgrades": FixtureTurn().Upgrades})
	case "/game/from-lobby/" + testLobbyID + "/generation":
		writeJSON(writer, 200, state)
	case "/game/from-lobby/" + testLobbyID + "/deadline":
		if deadline != nil {
			state = deadline()
			fake.setState(state)
		}
		writeJSON(writer, 200, state)
	case "/generation/commands":
		if answer != nil && answer(writer, request, body) {
			return
		}
		var batch map[string]any
		_ = json.Unmarshal(body, &batch)
		if batch["complete"] == true {
			completed := fake.stateOf(state["generation_id"].(uint), true, nil)
			fake.setState(completed)
			state = completed
		}
		writeJSON(writer, 200, state)
	default:
		writeJSON(writer, 404, map[string]any{"title": "Not Found", "status": 404, "detail": "no route"})
	}
}

func writeJSON(writer http.ResponseWriter, status int, value any) {
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(value)
}

// fakeRealtime is a realtime connection that acknowledges every subscription
// and lets a test publish on the channel.
type fakeRealtime struct {
	mutex  sync.Mutex
	events realtimeEvents
	closed bool
}

func (fake *fakeRealtime) dial(_, _ string, _ func() (string, error), events realtimeEvents) (realtimeConnection, error) {
	fake.mutex.Lock()
	defer fake.mutex.Unlock()
	fake.events = events
	go events.connected()
	return fake, nil
}

func (fake *fakeRealtime) Subscribe(string) error {
	go fake.events.subscribed()
	return nil
}

func (fake *fakeRealtime) Close() {
	fake.mutex.Lock()
	defer fake.mutex.Unlock()
	fake.closed = true
}

func (fake *fakeRealtime) publish(eventType string, state map[string]any) {
	data, _ := json.Marshal(map[string]any{"schema_version": 1, "type": eventType, "state": state})
	fake.mutex.Lock()
	events := fake.events
	fake.mutex.Unlock()
	events.publication(data)
}

// testClient is a client of the fake server, with fast retries.
func testClient(t *testing.T, server *fakeServer, realtime *fakeRealtime, play Play, options ...ClientOption) *Client {
	t.Helper()
	options = append([]ClientOption{
		WithLogOutput(nil),
		func(client *Client) {
			client.dial = realtime.dial
			client.timing.retryDelay = time.Millisecond
			client.timing.retryDelayMax = 5 * time.Millisecond
			client.timing.deadlineSlack = 10 * time.Millisecond
			client.timing.deadlineRetry = 20 * time.Millisecond
		},
	}, options...)
	client, err := NewClient(server.server.URL, "good", testLobbyID, play, options...)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close(context.Background()) })
	return client
}

func (fake *fakeServer) batchesSent() []map[string]any {
	fake.mutex.Lock()
	defer fake.mutex.Unlock()
	decoded := []map[string]any{}
	for _, body := range fake.batches {
		var batch map[string]any
		_ = json.Unmarshal(body, &batch)
		decoded = append(decoded, batch)
	}
	return decoded
}

func eventually(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !condition() {
		if time.Now().After(deadline) {
			t.Fatal("the condition never held")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func waitFor(t *testing.T, client *Client) error {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := client.Wait(ctx)
	if errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("the client never stopped")
	}
	return err
}

func TestClientPlaysATurnAndReportsTheEnd(t *testing.T) {
	server, realtime := newFakeServer(t), &fakeRealtime{}
	var turns []Turn
	var mutex sync.Mutex
	play := func(ctx context.Context, turn Turn) (Result, error) {
		mutex.Lock()
		turns = append(turns, turn)
		mutex.Unlock()
		Logger(ctx).Info("attacking", "castle", 2)
		memory, _ := NewMemory(map[string]int{"turns": 1})
		return Result{Commands: []Command{SendSoldierGroup(0, 1, 5)}, Logs: []LogLine{LogAt(LogWarning, "returned")}, Memory: memory}, nil
	}
	var ended []any
	client := testClient(t, server, realtime, play, WithGameEnded(func(_ context.Context, outcome GameOutcome, generation Generation) {
		mutex.Lock()
		defer mutex.Unlock()
		ended = append(ended, outcome.Reason, generation.ID)
	}))
	generation, err := client.Start(context.Background())
	if err != nil || generation.ID != 1 || client.TeamColor() != 1 {
		t.Fatalf("got %v, %v, team %d", generation.ID, err, client.TeamColor())
	}
	eventually(t, func() bool { return len(server.batchesSent()) == 1 })
	batch := server.batchesSent()[0]
	if batch["complete"] != true || batch["game_id"] != 7.0 || batch["generation_id"] != 1.0 {
		t.Fatalf("got batch %v", batch)
	}
	encoded, _ := json.Marshal([]any{batch["commands"], batch["memory"], batch["logs"]})
	want := `[[{"command_type":"send_soldier_group","parameters":{"destination_castle_id":1,"origin_castle_id":0,"soldier_amount":5}}],{"turns":1},` +
		`[{"level":"info","logger":"island_battles.player","message":"attacking castle=2"},{"level":"warning","logger":"island_battles.player","message":"returned"}]]`
	if string(encoded) != want {
		t.Fatalf("got %s, want %s", encoded, want)
	}
	if mutex.Lock(); len(turns) != 1 || turns[0].GameID != 7 || len(turns[0].Upgrades) == 0 {
		t.Fatalf("got turns %+v", turns)
	}
	mutex.Unlock()
	// A duplicate push of the completed generation doesn't play it again.
	realtime.publish(nextGenerationEvent, server.stateOf(1, false, nil))
	winner := uint(1)
	realtime.publish(gameEndedEvent, server.stateOf(2, false, map[string]any{"reason": "last_team_standing", "winner_team_color": winner, "summary": "Team 1 won"}))
	if err := waitFor(t, client); err != nil {
		t.Fatal(err)
	}
	mutex.Lock()
	defer mutex.Unlock()
	if len(turns) != 1 || len(ended) != 2 || ended[0] != LastTeamStanding || ended[1] != uint(2) {
		t.Fatalf("got %d turns and ended %v", len(turns), ended)
	}
	if outcome := client.Outcome(); outcome == nil || *outcome.WinnerTeamColor != 1 {
		t.Fatalf("got outcome %v", outcome)
	}
}

func TestClientRetriesALostBatchIdentically(t *testing.T) {
	server, realtime := newFakeServer(t), &fakeRealtime{}
	dropped := false
	server.answer = func(writer http.ResponseWriter, _ *http.Request, _ []byte) bool {
		if dropped {
			return false
		}
		dropped = true
		connection, _, err := writer.(http.Hijacker).Hijack()
		if err == nil {
			_ = connection.Close()
		}
		return true
	}
	client := testClient(t, server, realtime, func(context.Context, Turn) (Result, error) {
		return Result{Commands: []Command{SendSoldierGroup(0, 1, 4)}}, nil
	})
	if _, err := client.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	eventually(t, func() bool { return len(server.batchesSent()) == 2 })
	server.mutex.Lock()
	first, retry := server.batches[0], server.batches[1]
	server.mutex.Unlock()
	if !bytes.Equal(first, retry) {
		t.Fatalf("the retry differs:\n%s\n%s", first, retry)
	}
	time.Sleep(50 * time.Millisecond)
	if sent := len(server.batchesSent()); sent != 2 {
		t.Fatalf("sent %d batches", sent)
	}
}

func TestClientStopsOnARejectedResultAndKeepsTheTurnOpen(t *testing.T) {
	server, realtime := newFakeServer(t), &fakeRealtime{}
	server.answer = func(writer http.ResponseWriter, _ *http.Request, body []byte) bool {
		var batch map[string]any
		_ = json.Unmarshal(body, &batch)
		if batch["complete"] == true {
			writeJSON(writer, 409, map[string]any{"type": "urn:island-battles:problem:insufficient-soldiers", "title": "Conflict", "status": 409, "detail": "castle 0 has 3 soldiers"})
			return true
		}
		return false
	}
	client := testClient(t, server, realtime, func(ctx context.Context, _ Turn) (Result, error) {
		Logger(ctx).Info("before")
		return Result{Commands: []Command{SendSoldierGroup(0, 1, 40)}}, nil
	})
	if _, err := client.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	err := waitFor(t, client)
	var gameError *GameError
	if !errors.Is(err, ErrInsufficientSoldiers) || !errors.As(err, &gameError) || gameError.Status != 409 || err.Error() != "HTTP 409: castle 0 has 3 soldiers" {
		t.Fatalf("got %v", err)
	}
	batches := server.batchesSent()
	if len(batches) != 2 || batches[1]["complete"] != false || len(batches[1]["commands"].([]any)) != 0 {
		t.Fatalf("got batches %v", batches)
	}
	// The rejected batch's line is sent again with the error.
	logs := batches[1]["logs"].([]any)
	if len(logs) != 2 || logs[0].(map[string]any)["message"] != "before" || !strings.Contains(logs[1].(map[string]any)["message"].(string), "HTTP 409: castle 0 has 3 soldiers") {
		t.Fatalf("got logs %v", logs)
	}
}

func TestClientSendsAFailedTurnsTraceback(t *testing.T) {
	server, realtime := newFakeServer(t), &fakeRealtime{}
	client := testClient(t, server, realtime, func(context.Context, Turn) (Result, error) {
		panic(errors.New("scripted failure"))
	})
	if _, err := client.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	var failure botFailure
	if err := waitFor(t, client); !errors.As(err, &failure) || !failure.panicked {
		t.Fatalf("got %v", err)
	}
	batches := server.batchesSent()
	if len(batches) != 1 || batches[0]["complete"] != false || batches[0]["memory"] != nil {
		t.Fatalf("got %v", batches)
	}
	line := batches[0]["logs"].([]any)[0].(map[string]any)
	message := line["message"].(string)
	if line["logger"] != SDKLogger || line["level"] != "error" || !strings.HasPrefix(message, "the bot failed on generation 1\npanic: scripted failure\nTraceback (most recent call first):\n") ||
		!strings.Contains(message, "TestClientSendsAFailedTurnsTraceback") || len([]rune(message)) > MaxLogMessageLength {
		t.Fatalf("got %v", line)
	}
}

func TestClientRejectsAnInvalidResultBeforeSendingIt(t *testing.T) {
	server, realtime := newFakeServer(t), &fakeRealtime{}
	client := testClient(t, server, realtime, func(context.Context, Turn) (Result, error) {
		return Result{Memory: Memory(`{"blob":"` + strings.Repeat("x", MemoryLimitBytes) + `"}`)}, nil
	})
	if _, err := client.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	var gameError *GameError
	if err := waitFor(t, client); !errors.Is(err, ErrMemoryTooLarge) || errors.As(err, &gameError) {
		t.Fatalf("got %v", err)
	}
	if batches := server.batchesSent(); len(batches) != 1 || batches[0]["complete"] != false || batches[0]["memory"] != nil {
		t.Fatalf("got %v", batches)
	}
}

func TestClientResendsABatchWithoutTheLinesTheServerRefuses(t *testing.T) {
	server, realtime := newFakeServer(t), &fakeRealtime{}
	server.answer = func(writer http.ResponseWriter, _ *http.Request, body []byte) bool {
		if bytes.Contains(body, []byte(`"logs"`)) {
			writeJSON(writer, 422, map[string]any{"type": "urn:island-battles:problem:game-log-limit", "status": 422, "detail": "100 lines are stored"})
			return true
		}
		return false
	}
	var console bytes.Buffer
	client := testClient(t, server, realtime, func(ctx context.Context, _ Turn) (Result, error) {
		Logger(ctx).Info("one line too many")
		return Result{}, nil
	}, WithLogOutput(&syncWriter{writer: &console}))
	if _, err := client.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	eventually(t, func() bool { return len(server.batchesSent()) == 2 })
	batches := server.batchesSent()
	if batches[1]["logs"] != nil || batches[1]["complete"] != true || batches[0]["request_id"] == batches[1]["request_id"] {
		t.Fatalf("got %v", batches)
	}
	_ = client.Close(context.Background())
	if output := console.String(); strings.Count(output, "game log limit reached for generation 1") != 1 || !strings.Contains(output, "100 lines are stored") {
		t.Fatalf("got %q", output)
	}
}

// syncWriter makes a buffer safe for the client's goroutines.
type syncWriter struct {
	mutex  sync.Mutex
	writer io.Writer
}

func (writer *syncWriter) Write(data []byte) (int, error) {
	writer.mutex.Lock()
	defer writer.mutex.Unlock()
	return writer.writer.Write(data)
}

func TestClientAppliesTheTurnDeadlineWhileWaiting(t *testing.T) {
	server, realtime := newFakeServer(t), &fakeRealtime{}
	deadline := time.Now().Add(-time.Second)
	submitted := server.stateOf(1, true, nil)
	submitted["turn_deadline_at"] = deadline
	server.setState(submitted)
	attempts := 0
	server.deadline = func() map[string]any {
		attempts++
		if attempts == 1 {
			// The server's clock is behind: the turn is still open.
			return submitted
		}
		return server.stateOf(1, true, map[string]any{"reason": "turn_timeout", "winner_team_color": 1, "summary": "Team 2 didn't answer"})
	}
	ended := make(chan GameOutcome, 1)
	client := testClient(t, server, realtime, func(context.Context, Turn) (Result, error) {
		t.Error("a completed turn was played")
		return Result{}, nil
	}, WithGameEnded(func(_ context.Context, outcome GameOutcome, _ Generation) { ended <- outcome }))
	if _, err := client.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := waitFor(t, client); err != nil {
		t.Fatal(err)
	}
	if outcome := <-ended; outcome.Reason != TurnTimeout {
		t.Fatalf("got %v", outcome)
	}
	if server.mutex.Lock(); server.deadlines != 2 || len(server.batches) != 0 {
		t.Fatalf("got %d deadline posts and %d batches", server.deadlines, len(server.batches))
	}
	server.mutex.Unlock()
}

func TestClientDoesNotPlayTheBackendsSeat(t *testing.T) {
	server, realtime := newFakeServer(t), &fakeRealtime{}
	state := server.stateOf(1, false, nil)
	state["bot"] = map[string]any{"kind": "default", "level": 2}
	server.setState(state)
	client := testClient(t, server, realtime, func(context.Context, Turn) (Result, error) { return Result{}, nil })
	if _, err := client.Start(context.Background()); !errors.Is(err, ErrRejected) || !strings.Contains(err.Error(), "level 2 default bot") {
		t.Fatalf("got %v", err)
	}
	if _, err := client.Start(context.Background()); !errors.Is(err, ErrClientClosed) {
		t.Fatalf("got %v", err)
	}
}

func TestClientReportsARefusedCredential(t *testing.T) {
	server, realtime := newFakeServer(t), &fakeRealtime{}
	client, err := NewClient(server.server.URL, "bad", testLobbyID, func(context.Context, Turn) (Result, error) { return Result{}, nil }, WithLogOutput(nil), func(client *Client) { client.dial = realtime.dial })
	if err != nil {
		t.Fatal(err)
	}
	err = client.Run(context.Background())
	var gameError *GameError
	if !errors.Is(err, ErrAuthentication) || !errors.As(err, &gameError) || gameError.Status != 401 {
		t.Fatalf("got %v", err)
	}
}

func TestClientRecoversWithBackoffAfterFailedReads(t *testing.T) {
	server, realtime := newFakeServer(t), &fakeRealtime{}
	played := make(chan uint, 4)
	client := testClient(t, server, realtime, func(_ context.Context, turn Turn) (Result, error) {
		played <- turn.Generation.ID
		return Result{}, nil
	})
	if _, err := client.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if id := <-played; id != 1 {
		t.Fatalf("played %d", id)
	}
	eventually(t, func() bool { return len(server.batchesSent()) == 1 })
	// A generation missed while offline: the reads after the reconnect fail
	// twice, then succeed.
	server.mutex.Lock()
	server.failReads = 2
	server.state = server.stateOf(2, false, nil)
	server.mutex.Unlock()
	realtime.events.connected()
	select {
	case id := <-played:
		if id != 2 {
			t.Fatalf("played %d", id)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the missed generation was never played")
	}
	if server.mutex.Lock(); server.failReads != 0 {
		t.Fatalf("%d failures left", server.failReads)
	}
	server.mutex.Unlock()
}

func TestStateNeverGoesBackwards(t *testing.T) {
	server := newFakeServer(t)
	client, err := NewClient(server.server.URL, "good", testLobbyID, func(context.Context, Turn) (Result, error) { return Result{}, nil })
	if err != nil {
		t.Fatal(err)
	}
	install := func(state map[string]any) generationState {
		t.Helper()
		data, _ := json.Marshal(state)
		parsed, err := parseState(data)
		if err != nil {
			t.Fatal(err)
		}
		client.mutex.Lock()
		defer client.mutex.Unlock()
		installed, err := client.installLocked(parsed)
		if err != nil {
			t.Fatal(err)
		}
		return installed
	}
	install(server.stateOf(2, false, nil))
	submitted := server.stateOf(2, true, nil)
	submitted["log_lines_stored"] = 5
	install(submitted)
	if state := install(server.stateOf(2, false, nil)); !state.Submitted || state.LogLinesStored != 5 {
		t.Fatalf("a late read reopened the turn: %+v", state)
	}
	if state := install(server.stateOf(1, false, nil)); state.GenerationID != 2 {
		t.Fatalf("an older generation was installed: %+v", state)
	}
	ended := install(server.stateOf(2, false, map[string]any{"reason": "turn_timeout", "winner_team_color": nil, "summary": "Draw"}))
	if !ended.Submitted || ended.Outcome == nil {
		t.Fatalf("the deadline's outcome was lost: %+v", ended)
	}
	if state := install(server.stateOf(2, true, nil)); state.Outcome == nil {
		t.Fatal("a late read undid the outcome")
	}
	if len(client.queue) != 2 {
		t.Fatalf("queued %d states", len(client.queue))
	}
	foreign := server.stateOf(3, false, nil)
	foreign["lobby_id"] = "00000000-0000-4000-8000-000000000000"
	data, _ := json.Marshal(foreign)
	parsed, _ := parseState(data)
	client.mutex.Lock()
	_, err = client.installLocked(parsed)
	client.mutex.Unlock()
	if !errors.Is(err, ErrProtocol) {
		t.Fatalf("got %v", err)
	}
}

func TestEventsMustBeValid(t *testing.T) {
	server := newFakeServer(t)
	for _, data := range []string{
		`not an event`,
		`{"schema_version": 1, "type": "lobby.changed"}`,
		`{"schema_version": 2, "type": "game.next_generation", "state": {}}`,
		`{"schema_version": 1, "type": "game.ended", "state": ` + mustJSON(t, server.stateOf(2, false, nil)) + `}`,
	} {
		if _, err := parseEvent([]byte(data)); err == nil {
			t.Errorf("accepted %s", data)
		}
	}
	if _, err := parseEvent([]byte(`{"schema_version": 1, "type": "game.next_generation", "state": ` + mustJSON(t, server.stateOf(2, false, nil)) + `}`)); err != nil {
		t.Fatal(err)
	}
}

func mustJSON(t *testing.T, value any) string {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestResponseErrorsMapOntoKinds(t *testing.T) {
	for _, test := range []struct {
		status      int
		problemType string
		kind        error
	}{
		{401, "", ErrAuthentication},
		{429, "", ErrTransport},
		{503, "", ErrTransport},
		{422, "urn:island-battles:problem:castle-not-found", ErrCastleNotFound},
		{403, "urn:island-battles:problem:castle-not-owned", ErrCastleNotOwned},
		{409, "urn:island-battles:problem:upgrade-max-level", ErrUpgradeMaxLevel},
		{409, "urn:island-battles:problem:insufficient-soldiers", ErrInsufficientSoldiers},
		{409, "urn:island-battles:problem:game-ended", ErrGameEnded},
		{422, "urn:island-battles:problem:game-log-limit", ErrGameLogLimit},
		{422, "urn:island-battles:problem:bot-memory-too-large", ErrMemoryTooLarge},
		{422, "about:blank", ErrRejected},
		{404, "", ErrRejected},
	} {
		err := responseError(test.status, test.problemType, "")
		if !errors.Is(err, test.kind) || err.Status != test.status || err.Error() != fmt.Sprintf("HTTP %d: request failed (%d)", test.status, test.status) {
			t.Errorf("%d %s: got %v", test.status, test.problemType, err)
		}
	}
}

func TestRunPlaysASeatToTheEnd(t *testing.T) {
	server, realtime := newFakeServer(t), &fakeRealtime{}
	logFile := filepath.Join(t.TempDir(), "bot.log")
	play := func(ctx context.Context, turn Turn) (Result, error) {
		Logger(ctx).Debug("not logged at info")
		Logger(ctx).Info("playing", "generation", turn.Generation.ID)
		go realtime.publish(gameEndedEvent, server.stateOf(2, false, map[string]any{"reason": "generation_cap", "winner_team_color": nil, "summary": "Draw"}))
		return Result{}, nil
	}
	var stdout, stderr bytes.Buffer
	status := runMain(context.Background(), invocation{
		name: "mybot", play: play, stdout: &syncWriter{writer: &stdout}, stderr: &stderr,
		clientOptions: []ClientOption{func(client *Client) { client.dial = realtime.dial }},
	}, []string{"run", "--base-url", server.server.URL, "--token=good", "--lobby-id", testLobbyID, "--log-file", logFile})
	if status != 0 || !strings.Contains(stdout.String(), "INFO island_battles.player: playing generation=1\n") || !strings.HasSuffix(stdout.String(), "game ended at generation 2: Draw\n") || strings.Contains(stdout.String(), "not logged") {
		t.Fatalf("got status %d, stdout %q, stderr %q", status, stdout.String(), stderr.String())
	}
	written, err := os.ReadFile(logFile)
	if err != nil || !strings.Contains(string(written), "playing generation=1") {
		t.Fatalf("got %q, %v", written, err)
	}
	if status, _, stderr := run(t, sender, "run", "--base-url", server.server.URL); status != 2 || !strings.Contains(stderr, "--token and --lobby-id are required") {
		t.Fatalf("got status %d, stderr %q", status, stderr)
	}
	if status, _, stderr := run(t, sender, "run", "--base-url", server.server.URL, "--token=bad", "--lobby-id", testLobbyID); status != 1 || !strings.Contains(stderr, "FAIL: HTTP 401: bad credential") {
		t.Fatalf("got status %d, stderr %q", status, stderr)
	}
}

func TestRunReadsATokenThatStartsWithADash(t *testing.T) {
	server := newFakeServer(t)
	// "--token=-abc" is one argument; the server answers 401 to a token it doesn't know, so reaching it proves the flag was read.
	if status, _, stderr := run(t, sender, "run", "--base-url", server.server.URL, "--token=-abc", "--lobby-id", testLobbyID); status != 1 || !strings.Contains(stderr, "FAIL: HTTP 401: bad credential") {
		t.Fatalf("got status %d, stderr %q", status, stderr)
	}
}

func TestLoggerAttributesAndLevels(t *testing.T) {
	var output bytes.Buffer
	outputs := &logOutputs{level: slog.LevelDebug, writers: []io.Writer{&output}}
	turn := newTurnLog(newGameLog(3, 0), outputs)
	logger := Logger(turn.context(context.Background())).With("plan", "attack now").WithGroup("castle")
	logger.Debug("d", "id", 4)
	logger.Warn("w")
	logger.Error("e", slog.Group("target", "id", 5))
	lines, _ := turn.gameLog.batch(true)
	want := []LogLine{
		{Level: LogDebug, Logger: PlayerLogger, Message: `d plan="attack now" castle.id=4`},
		{Level: LogWarning, Logger: PlayerLogger, Message: `w plan="attack now"`},
		{Level: LogError, Logger: PlayerLogger, Message: `e plan="attack now" castle.target.id=5`},
	}
	if mustJSON(t, lines) != mustJSON(t, want) {
		t.Fatalf("got %v", lines)
	}
	if !strings.Contains(output.String(), " WARNING island_battles.player: w plan=") {
		t.Fatalf("got %q", output.String())
	}
	turn.close()
	logger.Info("after the turn")
	if lines, _ := turn.gameLog.batch(true); len(lines) != 3 {
		t.Fatalf("a line logged after the turn was filed: %v", lines)
	}
}
