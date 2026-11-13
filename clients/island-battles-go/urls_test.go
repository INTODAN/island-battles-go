package islandbattles

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// origin is one server of a split setup: it serves the fake server's routes
// and records the paths it was sent.
type origin struct {
	server *httptest.Server
	mutex  sync.Mutex
	paths  []string
}

func newOrigin(t *testing.T, fake *fakeServer) *origin {
	t.Helper()
	recorded := &origin{}
	recorded.server = httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		recorded.mutex.Lock()
		recorded.paths = append(recorded.paths, request.URL.Path)
		recorded.mutex.Unlock()
		fake.handle(writer, request)
	}))
	t.Cleanup(recorded.server.Close)
	return recorded
}

func (recorded *origin) sent() []string {
	recorded.mutex.Lock()
	defer recorded.mutex.Unlock()
	return append([]string(nil), recorded.paths...)
}

// The lobby and the game are separate sites (#228): the client sends the
// lobby's routes, with the guest credential, to the lobby's URL, and the
// game's, with a player token, to the game's.
func TestTheClientSendsEachRouteToItsOwnSite(t *testing.T) {
	fake, realtime := newFakeServer(t), &fakeRealtime{}
	game, lobby := newOrigin(t, fake), newOrigin(t, fake)
	play := func(_ context.Context, turn Turn) (Result, error) {
		go realtime.publish(gameEndedEvent, fake.stateOf(2, false, map[string]any{"reason": "generation_cap", "winner_team_color": nil, "summary": "Draw"}))
		return Result{}, nil
	}
	var stdout, stderr bytes.Buffer
	status := runMain(context.Background(), invocation{
		name: "mybot", play: play, stdout: &syncWriter{writer: &stdout}, stderr: &stderr,
		clientOptions: []ClientOption{func(client *Client) { client.dial = realtime.dial }},
		lookupEnv:     func(string) (string, bool) { return "", false },
	}, []string{"run", "--game-url", game.server.URL, "--lobby-url", lobby.server.URL + "/", "--token=good", "--lobby-id", testLobbyID})
	if status != 0 {
		t.Fatalf("got status %d, stdout %q, stderr %q", status, stdout.String(), stderr.String())
	}
	lobbyPaths, gamePaths := lobby.sent(), game.sent()
	if len(lobbyPaths) == 0 || len(gamePaths) == 0 {
		t.Fatalf("lobby %v, game %v", lobbyPaths, gamePaths)
	}
	for _, path := range lobbyPaths {
		if !strings.HasPrefix(path, "/lobby/v1/") {
			t.Errorf("the lobby was sent %s", path)
		}
	}
	for _, path := range gamePaths {
		if strings.HasPrefix(path, "/lobby/") {
			t.Errorf("the game was sent %s", path)
		}
	}
}

// run reads the URLs from --game-url and --lobby-url, from
// ISLAND_BATTLES_GAME_URL and ISLAND_BATTLES_LOBBY_URL, or from the
// shorthand --base-url, which sets both.
func TestRunReadsTheGameAndLobbyURLs(t *testing.T) {
	fake := newFakeServer(t)
	game, lobby := newOrigin(t, fake), newOrigin(t, fake)
	runWith := func(environment map[string]string, arguments ...string) (int, string) {
		var stdout, stderr bytes.Buffer
		status := runMain(context.Background(), invocation{
			name: "mybot", play: sender, stdout: &stdout, stderr: &stderr,
			lookupEnv: func(name string) (string, bool) { value, found := environment[name]; return value, found },
		}, append([]string{"run", "--token=bad", "--lobby-id", testLobbyID}, arguments...))
		return status, stderr.String()
	}
	// A bad credential fails at the lobby, which proves where the client went.
	environment := map[string]string{GameURLEnv: game.server.URL, LobbyURLEnv: lobby.server.URL}
	if status, stderr := runWith(environment); status != 1 || !strings.Contains(stderr, "HTTP 401") || len(lobby.sent()) == 0 || len(game.sent()) != 0 {
		t.Fatalf("from the environment: status %d, stderr %q, lobby %v, game %v", status, stderr, lobby.sent(), game.sent())
	}
	// A flag wins over the environment.
	before := len(lobby.sent())
	if status, stderr := runWith(map[string]string{GameURLEnv: game.server.URL, LobbyURLEnv: "http://127.0.0.1:9"}, "--lobby-url", lobby.server.URL); status != 1 ||
		!strings.Contains(stderr, "HTTP 401") || len(lobby.sent()) == before {
		t.Fatalf("from a flag: status %d, stderr %q", status, stderr)
	}
	before = len(lobby.sent())
	if status, stderr := runWith(nil, "--base-url", lobby.server.URL); status != 1 || !strings.Contains(stderr, "HTTP 401") || len(lobby.sent()) == before {
		t.Fatalf("from --base-url: status %d, stderr %q", status, stderr)
	}
	for _, arguments := range [][]string{
		{"--game-url", game.server.URL},
		{"--lobby-url", lobby.server.URL},
		{"--base-url", lobby.server.URL, "--game-url", game.server.URL},
	} {
		if status, stderr := runWith(nil, arguments...); status != 2 || !strings.Contains(stderr, "--base-url") {
			t.Errorf("%v: status %d, stderr %q", arguments, status, stderr)
		}
	}
}

func TestNewClientNeedsBothURLs(t *testing.T) {
	play := func(context.Context, Turn) (Result, error) { return Result{}, nil }
	for _, urls := range []URLs{{Game: "http://localhost:8888"}, {Lobby: "http://localhost:8890"}, {Game: "localhost", Lobby: "http://localhost:8890"}} {
		if _, err := NewClient(urls, "good", testLobbyID, play); err == nil {
			t.Errorf("%+v accepted", urls)
		}
	}
	if urls := BaseURL("http://localhost:8888"); urls.Game != urls.Lobby || urls.Game != "http://localhost:8888" {
		t.Fatalf("BaseURL = %+v", urls)
	}
}
