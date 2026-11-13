package lobbyadapter_test

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"islandBattles/backend/app"
	"islandBattles/backend/handlers"
	"islandBattles/backend/internal/testutils"
	"islandBattles/backend/lobbyadapter"
	"islandBattles/backend/routes"
	"islandBattles/backend/services"
	"islandBattles/lobby/application"
	"islandBattles/lobby/auth"
	"islandBattles/lobby/domain"
	"islandBattles/lobby/gamehooks"
	"islandBattles/lobby/httpapi"
	"islandBattles/lobby/mockgame"
	"islandBattles/lobby/playertoken"
	lobbypostgres "islandBattles/lobby/postgres"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/danielgtaylor/huma/v2/humatest"
	"github.com/go-chi/chi/v5"
	"gorm.io/gorm"
)

// mockGameKey is a second game of the stack's lobby, the lobby's mock game.
const mockGameKey = "mock-game"

// lobbyStack serves the lobby API and the Island Battles routes on one
// database, as the backend does, without Centrifugo. The lobby is configured
// with Island Battles as an HTTP game, as LOBBY_GAMES configures it: it calls
// Island Battles' signed hooks, and Island Battles reads it through its game
// API, both in memory through app.Loopback, as in the backend's process. The
// lobby's mock game, at a server of its own, is a second game.
type lobbyStack struct {
	t        *testing.T
	database *gorm.DB
	api      humatest.TestAPI
	// publisher keeps what the game pushed.
	publisher *testutils.RecordedPublisher
	// hookCalls counts the lobby's calls of each Island Battles hook.
	hookCalls map[string]int
}

func newLobbyStack(t *testing.T) *lobbyStack {
	t.Helper()
	return newLobbyStackWith(t, services.ServerBots{})
}

// newLobbyStackWith is newLobbyStack with the server's bots configured, as
// ISLAND_BATTLES_SERVER_BOTS does.
func newLobbyStackWith(t *testing.T, serverBots services.ServerBots) *lobbyStack {
	t.Helper()
	database := testutils.InitTestDB(t)
	stack := &lobbyStack{t: t, database: database, publisher: &testutils.RecordedPublisher{}, hookCalls: map[string]int{}}
	mock, err := mockgame.New(mockGameKey, []byte(strings.Repeat("m", 40)))
	if err != nil {
		t.Fatal(err)
	}
	mockServer := httptest.NewServer(mock)
	t.Cleanup(mockServer.Close)

	var hooks, gameAPI http.Handler
	loopback := &app.Loopback{}
	loopback.Handle(app.LoopbackOrigin+gamehooks.GameAPIPath, func() http.Handler { return gameAPI })
	loopback.Handle(app.LoopbackOrigin+lobbyadapter.HooksPath, func() http.Handler {
		return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
			stack.hookCalls[request.URL.Path[len(lobbyadapter.HooksPath)+1:]]++
			hooks.ServeHTTP(response, request)
		})
	})
	islandBattles, err := lobbyadapter.LobbyGame(app.LoopbackOrigin + lobbyadapter.HooksPath)
	if err != nil {
		t.Fatal(err)
	}
	mockEntry, err := json.Marshal(map[string]string{"key": mockGameKey, "name": "Mock game", "hooks_url": mockServer.URL + mockgame.HooksPath, "secret_env": "MOCK_GAME_SECRET"})
	if err != nil {
		t.Fatal(err)
	}
	secrets := map[string]string{lobbyadapter.SecretEnv: string(hookSecret), "MOCK_GAME_SECRET": strings.Repeat("m", 40)}
	games, err := gamehooks.ParseGames("["+string(islandBattles)+","+string(mockEntry)+"]", func(name string) (string, bool) {
		secret, found := secrets[name]
		return secret, found
	}, gamehooks.WithHTTPClient(loopback.Client()))
	if err != nil {
		t.Fatal(err)
	}
	clock := domain.SystemClock{}
	identifiers := domain.CryptoIdentifiers{}
	policy := domain.DefaultPolicy(gamehooks.Registrations(games)...)
	store, err := lobbypostgres.NewStore(database, clock)
	if err != nil {
		t.Fatal(err)
	}
	identities, err := lobbypostgres.NewIdentityStore(database)
	if err != nil {
		t.Fatal(err)
	}
	authentication, err := auth.NewService(identities, clock, policy, identifiers, []byte(strings.Repeat("s", 32)), "ws://127.0.0.1:1/connection/websocket")
	if err != nil {
		t.Fatal(err)
	}
	lobbies, err := application.NewService(store, clock, identifiers, policy)
	if err != nil {
		t.Fatal(err)
	}
	gameAPIRouter := chi.NewMux()
	gameAPIConfig := huma.DefaultConfig("Lobby game API", "1.0.0")
	gameAPIConfig.OpenAPIPath, gameAPIConfig.DocsPath, gameAPIConfig.SchemasPath = "", "", ""
	httpapi.RegisterGameAPI(humachi.New(gameAPIRouter, gameAPIConfig), lobbies, gamehooks.Secrets(games))
	gameAPI = gameAPIRouter
	lobbyClient, err := gamehooks.NewGameAPIClient(lobbyadapter.GameKey, app.LoopbackOrigin, hookSecret, gamehooks.WithHTTPClient(loopback.Client()))
	if err != nil {
		t.Fatal(err)
	}
	if hooks, err = lobbyadapter.Hooks(hookSecret, lobbyadapter.Registration(database, stack.publisher, nil, serverBots, lobbyClient)); err != nil {
		t.Fatal(err)
	}
	// The game verifies the lobby's player tokens in process, as the backend does.
	keys, err := playertoken.ParseKeySet("test:" + base64.RawURLEncoding.EncodeToString([]byte(strings.Repeat("k", 32))))
	if err != nil {
		t.Fatal(err)
	}
	playerTokens, err := auth.NewPlayerTokens(keys, "https://lobby.example.test", playertoken.MaxLifetime, clock, policy)
	if err != nil {
		t.Fatal(err)
	}
	verifier, err := playertoken.NewVerifier("https://lobby.example.test", keys)
	if err != nil {
		t.Fatal(err)
	}
	_, api := humatest.New(t, routes.NewAPIConfig("test", "1.0.0"))
	httpapi.Register(api, lobbies, authentication, authentication, playerTokens, nil)
	routes.SetupGameRoutes(api, &handlers.LobbyGame{Database: database, Players: handlers.PlayerTokens{Verifier: verifier}, Publisher: stack.publisher, Watchers: lobbyClient})
	routes.SetupRoutes(api, &handlers.Env{DB: database, ServerBots: serverBots.Enabled, BotCompute: serverBots.Compute}, "")
	stack.api = api
	return stack
}
