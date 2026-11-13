package app_test

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"islandBattles/backend/app"
	"islandBattles/backend/dal"
	"islandBattles/backend/dependencies"
	"islandBattles/backend/internal/testutils"
	"islandBattles/backend/lobbyadapter"
	"islandBattles/backend/models"
	"islandBattles/backend/retention"
	lobbyapp "islandBattles/lobby/app"
	"islandBattles/lobby/domain"
	"islandBattles/lobby/gamehooks"
	"islandBattles/lobby/playertoken"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// pushDelay is how long the slow Centrifugo takes to answer each API request.
// A game that only Go bots play pushes every generation, so playing its 300
// generations takes far longer than the start hook's timeout, on any machine.
const pushDelay = 30 * time.Millisecond

// slowCentrifugo is fakeCentrifugo, answering each API request after
// pushDelay, as a real Centrifugo over the network takes a while.
func slowCentrifugo(t *testing.T) string {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		body, _ := io.ReadAll(request.Body)
		time.Sleep(pushDelay)
		commands := strings.Count(strings.TrimSpace(string(body)), "\n") + 1
		_, _ = io.WriteString(response, strings.TrimSuffix(strings.Repeat(`{"result":{}}`+"\n", commands), "\n"))
	}))
	t.Cleanup(server.Close)
	return server.URL + "/api"
}

// startStack is a lobby and Island Battles, at lobbyURL and gameURL, which
// are the same URL while the lobby runs in the backend's process, with the
// game's database and the lobby's.
type startStack struct {
	lobbyURL, gameURL string
	game, lobby       *gorm.DB
}

// inProcessStack runs the lobby in the backend's process, as production does
// until the cutover: the lobby calls the hooks in memory.
func inProcessStack(t *testing.T) startStack {
	t.Helper()
	database, lobbyDatabase := testutils.InitTestDatabases(t)
	handler, err := app.New(dependencies.Config{
		Database:                  dependencies.DefaultDatabaseConfig(databaseURL(t, database)),
		CentrifugoPublicURL:       "ws://127.0.0.1:9/connection/websocket",
		CentrifugoAPIURL:          slowCentrifugo(t),
		CentrifugoAPIKey:          "test-centrifugo-api-key",
		ConnectionTokenHMACSecret: "test-connection-token-secret-of-sufficient-length",
		LobbyProxySecret:          "test-lobby-proxy-secret",
		SubmitTokenSecret:         "test-submit-token-secret",
		GameRetentionPeriod:       retention.DefaultPeriod,
		PlayerTokenKeys:           playerTokenKeys(t),
		PlayerTokenLifetime:       playertoken.MaxLifetime,
		LobbyHookSecret:           []byte(hookSecret),
		LobbyGameSecrets:          map[string]string{lobbyadapter.SecretEnv: hookSecret},
	}, database, lobbyDatabase, discard)
	if err != nil {
		t.Fatal(err)
	}
	backend := httptest.NewServer(handler)
	t.Cleanup(backend.Close)
	return startStack{lobbyURL: backend.URL, gameURL: backend.URL, game: database, lobby: lobbyDatabase}
}

// splitStack runs the lobby as a server of its own, which calls the
// backend's hooks over the network, as local development and the live tests
// do. wrap, if not nil, wraps the backend's handler.
func splitStack(t *testing.T, wrap func(http.Handler) http.Handler) startStack {
	t.Helper()
	database, lobbyTestDatabase := testutils.InitTestDatabases(t)
	centrifugo := slowCentrifugo(t)
	var backendHandler, lobbyHandler atomic.Value
	backend := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		backendHandler.Load().(http.Handler).ServeHTTP(response, request)
	}))
	t.Cleanup(backend.Close)
	lobbyServer := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		lobbyHandler.Load().(http.Handler).ServeHTTP(response, request)
	}))
	t.Cleanup(lobbyServer.Close)

	games, err := (dependencies.Config{PublicURL: backend.URL}).LobbyGamesConfig("")
	if err != nil {
		t.Fatal(err)
	}
	lobbyDatabase, err := lobbyapp.OpenDatabase(lobbyapp.DatabaseConfig{URL: databaseURL(t, lobbyTestDatabase), MaxOpenConnections: 10, MaxIdleConnections: 2}, discard)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if pool, err := lobbyDatabase.DB(); err == nil {
			_ = pool.Close()
		}
	})
	lobbyConfig := lobbyapp.Config{
		PublicURL: lobbyServer.URL, CentrifugoAPIURL: centrifugo, CentrifugoAPIKey: "test-centrifugo-api-key", CentrifugoPublicURL: "ws://127.0.0.1:9/connection/websocket",
		ConnectionTokenSecret: "test-connection-token-secret-of-sufficient-length", ProxySecret: "test-lobby-proxy-secret",
		PlayerTokenKeys: playerTokenKeys(t), Games: games, GameSecrets: map[string]string{lobbyadapter.SecretEnv: hookSecret},
	}
	lobby, err := lobbyapp.New(lobbyConfig, lobbyDatabase, lobbyapp.NewCentrifugoClient(lobbyConfig), discard)
	if err != nil {
		t.Fatal(err)
	}
	handler, err := lobby.Handler()
	if err != nil {
		t.Fatal(err)
	}
	lobbyHandler.Store(handler)

	config, err := dependencies.LoadConfigFromLookup(func(name string) (string, bool) {
		value, found := map[string]string{
			dependencies.DatabaseURLEnv:       databaseURL(t, database),
			dependencies.CentrifugoAPIURLEnv:  centrifugo,
			dependencies.CentrifugoAPIKeyEnv:  "test-centrifugo-api-key",
			dependencies.SubmitTokenSecretEnv: "test-submit-token-secret",
			dependencies.LobbyHookSecretEnv:   hookSecret,
			dependencies.LobbyURLEnv:          lobbyServer.URL,
		}[name]
		return value, found
	})
	if err != nil {
		t.Fatal(err)
	}
	gameHandler, err := app.New(config, database, nil, discard)
	if err != nil {
		t.Fatal(err)
	}
	if wrap != nil {
		gameHandler = wrap(gameHandler)
	}
	backendHandler.Store(gameHandler)
	return startStack{lobbyURL: lobbyServer.URL, gameURL: backend.URL, game: database, lobby: lobbyTestDatabase}
}

// botLobby is a lobby whose host and only other seat both play default bots
// of level, ready to start.
type botLobby struct {
	host domain.GuestSession
	room domain.Snapshot
}

func (stack startStack) lobbyPath(room domain.Snapshot) string {
	return fmt.Sprintf("%s/lobby/v1/games/%s/lobbies/%s", stack.lobbyURL, room.Lobby.GameKey, room.Lobby.ID)
}

// expectStatus sends a request and checks its status, and decodes its body
// into into, unless into is nil.
func expectStatus(t *testing.T, want int, into any, method, url, token string, body any) {
	t.Helper()
	status, content, _ := send(t, method, url, token, body)
	if status != want {
		t.Fatalf("%s %s = %d, want %d: %s", method, url, status, want, content)
	}
	if into != nil {
		if err := json.Unmarshal(content, into); err != nil {
			t.Fatal(err)
		}
	}
}

func (stack startStack) allDefaultBotLobby(t *testing.T, level int) botLobby {
	t.Helper()
	var lobby botLobby
	expectStatus(t, http.StatusCreated, &lobby.host, http.MethodPost, stack.lobbyURL+"/lobby/v1/sessions", "", map[string]string{"display_name": "Alice"})
	token := lobby.host.AccessToken
	expectStatus(t, http.StatusCreated, &lobby.room, http.MethodPost, stack.lobbyURL+"/lobby/v1/lobbies", token, map[string]any{"request_id": uuid.NewString(), "game_key": lobbyadapter.GameKey,
		"name": "Bots", "teams": []map[string]string{{"name": "Red"}, {"name": "Blue"}}, "client_instance_id": uuid.NewString()})
	path := stack.lobbyPath(lobby.room)
	expectStatus(t, http.StatusOK, nil, http.MethodPut, path+"/team", token, map[string]string{"team_id": lobby.room.Teams[0].ID})
	expectStatus(t, http.StatusOK, nil, http.MethodPost, path+"/seats", token, map[string]any{"team_id": lobby.room.Teams[1].ID, "seat_settings": map[string]int{"bot_level": level}})
	expectStatus(t, http.StatusOK, nil, http.MethodPut, path+"/member-settings", token, map[string]any{"member_settings": map[string]any{"bot": map[string]any{"kind": "default", "level": level}}})
	expectStatus(t, http.StatusOK, nil, http.MethodPut, path+"/ready", token, map[string]bool{"ready": true})
	return lobby
}

// start starts the lobby, and returns the start's status and body.
func (stack startStack) start(t *testing.T, lobby botLobby) (int, string) {
	t.Helper()
	status, content, _ := send(t, http.MethodPost, stack.lobbyPath(lobby.room)+"/start", lobby.host.AccessToken, map[string]any{})
	return status, string(content)
}

// played is how many generations the lobby's game has, and whether it has
// ended.
func (stack startStack) played(t *testing.T, lobbyID string) (int64, bool) {
	t.Helper()
	game, err := dal.FindLobbyGame(stack.game, lobbyID)
	if err != nil {
		t.Fatal(err)
	}
	generations, err := dal.CountGenerationsForGame(stack.game, game.ID)
	if err != nil {
		t.Fatal(err)
	}
	return generations, game.Outcome != nil
}

// readState reads the host's state of the lobby's game, a game request,
// which first has the Go bots catch up.
func (stack startStack) readState(t *testing.T, lobby botLobby) {
	t.Helper()
	var token domain.PlayerToken
	expectStatus(t, http.StatusOK, &token, http.MethodPost, stack.lobbyURL+"/lobby/v1/player-tokens", lobby.host.AccessToken, map[string]string{"game_key": lobbyadapter.GameKey})
	expectStatus(t, http.StatusOK, nil, http.MethodGet, stack.gameURL+"/game/from-lobby/"+lobby.room.Lobby.ID+"/generation", token.Token, nil)
}

// The start hook plays the Go bots of a game that only they play for a few
// seconds at most, in both modes, so the lobby gets its answer within the
// hook's timeout; later game requests play the rest, a few seconds each.
func TestAnAllDefaultBotStartAnswersWithinTheHookTimeout(t *testing.T) {
	for _, mode := range []struct {
		name  string
		stack func(*testing.T) startStack
	}{
		{"in process", inProcessStack},
		{"split", func(t *testing.T) startStack { return splitStack(t, nil) }},
	} {
		t.Run(mode.name, func(t *testing.T) {
			stack := mode.stack(t)
			for level := 1; level <= 3; level++ {
				lobby := stack.allDefaultBotLobby(t, level)
				began := time.Now()
				status, content := stack.start(t, lobby)
				took := time.Since(began)
				// A hook that takes longer than its timeout is a 502.
				if status != http.StatusOK || took >= gamehooks.DefaultTimeout {
					t.Fatalf("level %d: start = %d after %v: %s", level, status, took, content)
				}
				generations, ended := stack.played(t, lobby.room.Lobby.ID)
				if ended || generations < 2 {
					t.Fatalf("level %d: the start played %d generations, ended %v", level, generations, ended)
				}
				stack.readState(t, lobby)
				if after, _ := stack.played(t, lobby.room.Lobby.ID); after <= generations {
					t.Fatalf("level %d: a game request played no turn: %d generations, then %d", level, generations, after)
				}
			}
		})
	}
}

// A start whose hook bound the game but whose answer the lobby never got is a
// 502, and the lobby's retried start binds the same game.
func TestARetriedStartBindsTheSameGame(t *testing.T) {
	var startHooks atomic.Int32
	stack := splitStack(t, func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
			if request.URL.Path != lobbyadapter.HooksPath+"/"+gamehooks.HookStart || startHooks.Add(1) > 1 {
				next.ServeHTTP(response, request)
				return
			}
			// The game binds the first start, and the answer is lost.
			next.ServeHTTP(httptest.NewRecorder(), request)
			http.Error(response, "lost", http.StatusBadGateway)
		})
	})
	lobby := stack.allDefaultBotLobby(t, 3)
	if status, content := stack.start(t, lobby); status != http.StatusBadGateway || !strings.Contains(content, "game_unavailable") {
		t.Fatalf("first start = %d %s", status, content)
	}
	first, err := dal.FindLobbyGame(stack.game, lobby.room.Lobby.ID)
	if err != nil {
		t.Fatal(err)
	}
	if status, content := stack.start(t, lobby); status != http.StatusOK {
		t.Fatalf("retried start = %d %s", status, content)
	}
	var games int64
	lobbyID := lobby.room.Lobby.ID
	if err := stack.game.Model(&models.Game{}).Where(&models.Game{LobbyID: &lobbyID}).Count(&games).Error; err != nil || games != 1 {
		t.Fatalf("the lobby has %d games: %v", games, err)
	}
	var boundRound int64
	if err := stack.lobby.Table("lobby_rooms").Where("id = ?", lobbyID).Select("bound_round").Scan(&boundRound).Error; err != nil || boundRound != 1 {
		t.Fatalf("bound round = %d: %v", boundRound, err)
	}
	if second, err := dal.FindLobbyGame(stack.game, lobbyID); err != nil || second.ID != first.ID || startHooks.Load() != 2 {
		t.Fatalf("retried game = %+v, %v after %d start hooks; first %d", second, err, startHooks.Load(), first.ID)
	}
}
