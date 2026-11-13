package app_test

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"islandBattles/backend/app"
	"islandBattles/backend/dependencies"
	"islandBattles/backend/internal/testutils"
	"islandBattles/backend/lobbyadapter"
	"islandBattles/backend/retention"
	lobbyapp "islandBattles/lobby/app"
	"islandBattles/lobby/domain"
	"islandBattles/lobby/playertoken"

	"github.com/google/uuid"
	gormpostgres "gorm.io/driver/postgres"
	"gorm.io/gorm"
)

const hookSecret = "test-lobby-hook-secret-of-32-bytes"

var discard = slog.New(slog.NewTextHandler(io.Discard, nil))

func playerTokenKeys(t *testing.T) *playertoken.KeySet {
	t.Helper()
	keys, err := playertoken.ParseKeySet("test:" + base64.RawURLEncoding.EncodeToString([]byte(strings.Repeat("k", 32))))
	if err != nil {
		t.Fatal(err)
	}
	return keys
}

func databaseURL(t *testing.T, database *gorm.DB) string {
	t.Helper()
	dialector, ok := database.Dialector.(*gormpostgres.Dialector)
	if !ok {
		t.Fatalf("unexpected dialector %T", database.Dialector)
	}
	return dialector.DSN
}

// fakeCentrifugo answers each command of a Centrifugo API request, one JSON
// command per line, with success.
func fakeCentrifugo(t *testing.T) string {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		body, _ := io.ReadAll(request.Body)
		commands := strings.Count(strings.TrimSpace(string(body)), "\n") + 1
		_, _ = io.WriteString(response, strings.TrimSuffix(strings.Repeat(`{"result":{}}`+"\n", commands), "\n"))
	}))
	t.Cleanup(server.Close)
	return server.URL + "/api"
}

// send makes a request and returns its status and body.
func send(t *testing.T, method, url, token string, body any) (int, []byte, http.Header) {
	t.Helper()
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		reader = bytes.NewReader(encoded)
	}
	request, err := http.NewRequest(method, url, reader)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = response.Body.Close() }()
	content, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	return response.StatusCode, content, response.Header
}

// In-process mode, production's until the cutover: without
// ISLAND_BATTLES_LOBBY_URL, the backend serves the lobby that lobby/app
// builds, which calls Island Battles' hooks in memory and keeps its tables in
// its own database.
func TestTheBackendRunsTheLobbyInItsOwnProcessWithoutALobbyURL(t *testing.T) {
	database, lobbyDatabase := testutils.InitTestDatabases(t)
	config := dependencies.Config{
		Database:                  dependencies.DefaultDatabaseConfig(databaseURL(t, database)),
		CentrifugoPublicURL:       "ws://127.0.0.1:9/connection/websocket",
		CentrifugoAPIURL:          fakeCentrifugo(t),
		CentrifugoAPIKey:          "test-centrifugo-api-key",
		ConnectionTokenHMACSecret: "test-connection-token-secret-of-sufficient-length",
		LobbyProxySecret:          "test-lobby-proxy-secret",
		SubmitTokenSecret:         "test-submit-token-secret",
		GameRetentionPeriod:       retention.DefaultPeriod,
		PlayerTokenKeys:           playerTokenKeys(t),
		PlayerTokenLifetime:       playertoken.MaxLifetime,
		LobbyHookSecret:           []byte(hookSecret),
		LobbyGameSecrets:          map[string]string{lobbyadapter.SecretEnv: hookSecret},
	}
	// The lobby in this process needs its own database.
	if _, err := app.New(config, database, nil, discard); err == nil {
		t.Fatal("the lobby ran in this process without its own database")
	}
	handler, err := app.New(config, database, lobbyDatabase, discard)
	if err != nil {
		t.Fatal(err)
	}
	backend := httptest.NewServer(handler)
	t.Cleanup(backend.Close)

	if status, _, header := send(t, http.MethodGet, backend.URL+"/", "", nil); status != http.StatusTemporaryRedirect || header.Get("Location") != lobbyapp.UIPath {
		t.Fatalf("/ = %d to %q", status, header.Get("Location"))
	}
	if status, content, _ := send(t, http.MethodGet, backend.URL+lobbyapp.UIPath+"games.json", "", nil); status != http.StatusOK || !strings.Contains(string(content), `"island-battles"`) {
		t.Fatalf("the lobby page's catalog = %d %s", status, content)
	}
	status, content, _ := send(t, http.MethodPost, backend.URL+"/lobby/v1/sessions", "", map[string]string{"display_name": "Alice"})
	if status != http.StatusCreated {
		t.Fatalf("create session = %d %s", status, content)
	}
	var alice domain.GuestSession
	if err := json.Unmarshal(content, &alice); err != nil {
		t.Fatal(err)
	}
	// The session is in the lobby's database, and Island Battles' has no
	// lobby table.
	var sessions int64
	if err := lobbyDatabase.Table("lobby_sessions").Count(&sessions).Error; err != nil || sessions != 1 {
		t.Fatalf("the lobby's database has %d sessions: %v", sessions, err)
	}
	if database.Migrator().HasTable("lobby_sessions") {
		t.Fatal("Island Battles' database has the lobby's tables")
	}
	// The lobby calls the game's validate-settings hook in memory: a map
	// that doesn't exist is the game's refusal, not an unavailable game.
	status, content, _ = send(t, http.MethodPost, backend.URL+"/lobby/v1/lobbies", alice.AccessToken, map[string]any{"request_id": uuid.NewString(), "game_key": lobbyadapter.GameKey,
		"name": "In process", "teams": []map[string]string{{"name": "Red"}, {"name": "Blue"}}, "client_instance_id": uuid.NewString(), "game_settings": map[string]any{"map_template_id": 999999}})
	if status != http.StatusUnprocessableEntity || !strings.Contains(string(content), "invalid_game_settings") {
		t.Fatalf("create lobby = %d %s", status, content)
	}
	var token domain.PlayerToken
	status, content, _ = send(t, http.MethodPost, backend.URL+"/lobby/v1/player-tokens", alice.AccessToken, map[string]string{"game_key": lobbyadapter.GameKey})
	if err := json.Unmarshal(content, &token); status != http.StatusOK || err != nil {
		t.Fatalf("player token = %d %s", status, content)
	}
	// The game verifies the token with the lobby's keys: a lobby without a
	// game is not found, where a bad token is 401.
	if status, content, _ := send(t, http.MethodGet, backend.URL+"/game/from-lobby/"+uuid.NewString()+"/generation", token.Token, nil); status != http.StatusNotFound {
		t.Fatalf("generation with a player token = %d %s", status, content)
	}
	if status, _, _ := send(t, http.MethodGet, backend.URL+"/game/from-lobby/"+uuid.NewString()+"/generation", alice.AccessToken, nil); status != http.StatusUnauthorized {
		t.Fatalf("generation with the guest credential = %d", status)
	}
}

// Remote mode, local development's and the live tests': with
// ISLAND_BATTLES_LOBBY_URL, the lobby is a server of its own. The backend
// serves none of its routes, serves the hooks that the lobby calls over the
// network, and verifies player tokens with the lobby's JWKS.
func TestTheBackendUsesALobbyOfItsOwnAtTheLobbyURL(t *testing.T) {
	database, lobbyTestDatabase := testutils.InitTestDatabases(t)
	centrifugo := fakeCentrifugo(t)
	// The lobby and the backend each need the other's URL.
	var backendHandler atomic.Value
	backend := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		backendHandler.Load().(http.Handler).ServeHTTP(response, request)
	}))
	t.Cleanup(backend.Close)

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
	var lobbyURL string
	lobbyConfig := lobbyapp.Config{
		CentrifugoAPIURL: centrifugo, CentrifugoAPIKey: "test-centrifugo-api-key", CentrifugoPublicURL: "ws://127.0.0.1:9/connection/websocket",
		ConnectionTokenSecret: "test-connection-token-secret-of-sufficient-length", ProxySecret: "test-lobby-proxy-secret",
		PlayerTokenKeys: playerTokenKeys(t), Games: games, GameSecrets: map[string]string{lobbyadapter.SecretEnv: hookSecret},
	}
	var lobbyHandler atomic.Value
	lobbyServer := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		lobbyHandler.Load().(http.Handler).ServeHTTP(response, request)
	}))
	t.Cleanup(lobbyServer.Close)
	lobbyURL = lobbyServer.URL
	lobbyConfig.PublicURL = lobbyURL
	lobby, err := lobbyapp.New(lobbyConfig, lobbyDatabase, lobbyapp.NewCentrifugoClient(lobbyConfig), discard)
	if err != nil {
		t.Fatal(err)
	}
	handler, err := lobby.Handler()
	if err != nil {
		t.Fatal(err)
	}
	lobbyHandler.Store(handler)

	// Remote mode needs none of the lobby's own settings.
	config, err := dependencies.LoadConfigFromLookup(func(name string) (string, bool) {
		value, found := map[string]string{
			dependencies.DatabaseURLEnv:       databaseURL(t, database),
			dependencies.CentrifugoAPIURLEnv:  centrifugo,
			dependencies.CentrifugoAPIKeyEnv:  "test-centrifugo-api-key",
			dependencies.SubmitTokenSecretEnv: "test-submit-token-secret",
			dependencies.LobbyHookSecretEnv:   hookSecret,
			dependencies.LobbyURLEnv:          lobbyURL,
			// The game's monthly compute guard applies in either mode.
			dependencies.BotComputeBudgetEnv: "1000",
			dependencies.BotComputeFakeEnv:   "1000",
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
	backendHandler.Store(gameHandler)

	for _, path := range []string{"/lobby/v1/sessions", "/lobby/ui/", "/lobby/v1/jwks.json", "/lobby/v1/game-api/games/island-battles/lobbies/" + uuid.NewString(), "/internal/lobby/centrifugo/subscribe", "/"} {
		method := http.MethodGet
		if strings.HasSuffix(path, "sessions") || strings.HasSuffix(path, "subscribe") {
			method = http.MethodPost
		}
		if status, content, _ := send(t, method, backend.URL+path, "", map[string]string{}); status != http.StatusNotFound && status != http.StatusMethodNotAllowed {
			t.Errorf("the backend serves %s %s: %d %s", method, path, status, content)
		}
	}
	status, content, _ := send(t, http.MethodPost, lobbyURL+"/lobby/v1/sessions", "", map[string]string{"display_name": "Alice"})
	if status != http.StatusCreated {
		t.Fatalf("create session = %d %s", status, content)
	}
	var alice domain.GuestSession
	if err := json.Unmarshal(content, &alice); err != nil {
		t.Fatal(err)
	}
	// The lobby calls the backend's validate-settings hook over the network,
	// signed with the shared secret.
	status, content, _ = send(t, http.MethodPost, lobbyURL+"/lobby/v1/lobbies", alice.AccessToken, map[string]any{"request_id": uuid.NewString(), "game_key": lobbyadapter.GameKey,
		"name": "Remote", "teams": []map[string]string{{"name": "Red"}, {"name": "Blue"}}, "client_instance_id": uuid.NewString(), "game_settings": map[string]any{"map_template_id": 999999}})
	if status != http.StatusUnprocessableEntity || !strings.Contains(string(content), "invalid_game_settings") {
		t.Fatalf("create lobby = %d %s", status, content)
	}
	var token domain.PlayerToken
	status, content, _ = send(t, http.MethodPost, lobbyURL+"/lobby/v1/player-tokens", alice.AccessToken, map[string]string{"game_key": lobbyadapter.GameKey})
	if err := json.Unmarshal(content, &token); status != http.StatusOK || err != nil {
		t.Fatalf("player token = %d %s", status, content)
	}
	// The backend verifies the lobby's token with the lobby's JWKS.
	if status, content, _ := send(t, http.MethodGet, backend.URL+"/game/from-lobby/"+uuid.NewString()+"/generation", token.Token, nil); status != http.StatusNotFound {
		t.Fatalf("generation with a player token = %d %s", status, content)
	}
	if status, _, _ := send(t, http.MethodGet, backend.URL+"/game/from-lobby/"+uuid.NewString()+"/generation", alice.AccessToken, nil); status != http.StatusUnauthorized {
		t.Fatalf("generation with the guest credential = %d", status)
	}
	// The game guards the bot runners' compute itself: with the month's
	// budget spent, games with uploaded bots may not start.
	status, content, _ = send(t, http.MethodGet, backend.URL+"/game/bot-compute", "", nil)
	var compute struct {
		UsedGBSeconds   float64 `json:"used_gb_seconds"`
		BudgetGBSeconds float64 `json:"budget_gb_seconds"`
		Available       bool    `json:"available"`
	}
	if err := json.Unmarshal(content, &compute); status != http.StatusOK || err != nil || compute.UsedGBSeconds != 1000 || compute.BudgetGBSeconds != 1000 || compute.Available {
		t.Fatalf("bot compute = %d %s", status, content)
	}
}
