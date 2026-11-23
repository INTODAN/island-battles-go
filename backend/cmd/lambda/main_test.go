package main

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/INTODAN/mpl/domain"
	"github.com/INTODAN/mpl/playertoken"
	"islandBattles/backend/app"
	"islandBattles/backend/dependencies"
	"islandBattles/backend/internal/testutils"
	"islandBattles/backend/models"
	"islandBattles/backend/observability"
	"islandBattles/backend/retention"
	"islandBattles/backend/services"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambdaurl"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	gormpostgres "gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Lambda caps buffered responses at 6 MB. Streamed responses may reach 200 MB.
const bufferedResponseLimit = 6 << 20

type functionURL func(context.Context, *events.LambdaFunctionURLRequest) (*events.LambdaFunctionURLStreamingResponse, error)

// call invokes the handler the way the Lambda runtime does for a Function URL
// in RESPONSE_STREAM mode, and reads the whole streamed body.
func call(t *testing.T, invoke functionURL, method, path, body, authorization string) (*events.LambdaFunctionURLStreamingResponse, []byte) {
	t.Helper()
	return callWithHeaders(t, invoke, method, path, body, authorization, nil)
}

// callWithHeaders is call with more request headers, in lower case as the
// Function URL passes them.
func callWithHeaders(t *testing.T, invoke functionURL, method, path, body, authorization string, headers map[string]string) (*events.LambdaFunctionURLStreamingResponse, []byte) {
	t.Helper()
	request := &events.LambdaFunctionURLRequest{
		RawPath: path,
		Headers: map[string]string{"content-type": "application/json"},
		Body:    body,
		RequestContext: events.LambdaFunctionURLRequestContext{
			DomainName: "example.lambda-url.eu-west-1.on.aws",
			HTTP:       events.LambdaFunctionURLRequestContextHTTPDescription{Method: method, Path: path, SourceIP: "203.0.113.7"},
		},
	}
	if authorization != "" {
		request.Headers["authorization"] = authorization
	}
	for name, value := range headers {
		request.Headers[name] = value
	}
	response, err := invoke(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	content, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	return response, content
}

// testConfig configures the backend with testLobby's URL.
func testConfig(t *testing.T, database *gorm.DB, lobby testLobby) dependencies.Config {
	t.Helper()
	dialector, ok := database.Dialector.(*gormpostgres.Dialector)
	if !ok {
		t.Fatalf("unexpected dialector %T", database.Dialector)
	}
	// Nothing in this test pushes to Centrifugo, so its address is unused.
	return dependencies.Config{
		Database:            dependencies.DefaultDatabaseConfig(dialector.DSN),
		CentrifugoAPIURL:    "http://127.0.0.1:9/api",
		CentrifugoAPIKey:    "test-centrifugo-api-key",
		SubmitTokenSecret:   "test-submit-token-secret",
		GameRetentionPeriod: retention.DefaultPeriod,
		LobbyHookSecret:     []byte(testHookSecret),
		LobbyURL:            lobby.url,
	}
}

// testLobby stands in for the lobby, which runs on its own: it publishes its
// keys at its JWKS URL, and signs player tokens with them.
type testLobby struct {
	url  string
	keys *playertoken.KeySet
}

func startTestLobby(t *testing.T) testLobby {
	t.Helper()
	keys := testPlayerTokenKeys(t)
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path != playertoken.JWKSPath {
			http.NotFound(response, request)
			return
		}
		response.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(response).Encode(keys.JWKS())
	}))
	t.Cleanup(server.Close)
	return testLobby{url: server.URL, keys: keys}
}

// playerToken is the lobby's player token for playerID in Island Battles.
func (lobby testLobby) playerToken(t *testing.T, playerID string) string {
	t.Helper()
	now := time.Now()
	token, err := lobby.keys.Sign(playertoken.Claims{Issuer: lobby.url, Subject: playerID, Audience: "island-battles", Name: "Alice",
		IssuedAt: now.Unix(), Expires: now.Add(playertoken.MaxLifetime).Unix()})
	if err != nil {
		t.Fatal(err)
	}
	return token
}

// testHookSecret signs the lobby's calls of the game's hooks, and the game's
// of the lobby's game API.
const testHookSecret = "test-lobby-hook-secret-of-32-bytes"

func testPlayerTokenKeys(t *testing.T) *playertoken.KeySet {
	t.Helper()
	keys, err := playertoken.ParseKeySet("test:" + base64.RawURLEncoding.EncodeToString([]byte(strings.Repeat("k", 32))))
	if err != nil {
		t.Fatal(err)
	}
	return keys
}

func TestReplayOverSixMegabytesStreamsThroughTheFunctionURL(t *testing.T) {
	database := testutils.InitTestDB(t)
	lobby := startTestLobby(t)
	handler, err := newHandler(testConfig(t, database, lobby), database, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	invoke := lambdaurl.Wrap(handler)

	alice := uuid.NewString()
	lobbyID, red, blue := uuid.NewString(), uuid.NewString(), uuid.NewString()
	binding, err := services.RunLobbyGame(context.Background(), database, &testutils.RecordedPublisher{}, nil, domain.Snapshot{
		Lobby:       domain.Lobby{ID: lobbyID, GameKey: "island-battles", Phase: domain.PhaseStarted, GameSettings: testutils.CircleMapSettings(2, "")},
		Teams:       []domain.Team{{ID: red, Name: "Red", Order: 1}, {ID: blue, Name: "Blue", Order: 2}},
		StartRoster: domain.StartRoster{{PlayerID: alice, TeamID: red, TeamOrder: 1}, {PlayerID: uuid.NewString(), TeamID: blue, TeamOrder: 2}},
	})
	if err != nil {
		t.Fatal(err)
	}
	enlargeGame(t, database, binding.GameID)

	// The game's routes take a player token from the lobby, which the game
	// verifies with the lobby's JWKS.
	playerToken := lobby.playerToken(t, alice)
	if response, content := call(t, invoke, http.MethodGet, "/game/from-lobby/"+lobbyID+"/replay", "", "Bearer not-a-player-token"); response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("replay without a player token=%d %.200s", response.StatusCode, content)
	}
	response, content := call(t, invoke, http.MethodGet, "/game/from-lobby/"+lobbyID+"/replay", "", "Bearer "+playerToken)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("replay=%d %.500s", response.StatusCode, content)
	}
	if !strings.HasPrefix(response.Headers["Content-Type"], "text/html") {
		t.Fatalf("replay content type=%q", response.Headers["Content-Type"])
	}
	if len(content) <= bufferedResponseLimit {
		t.Fatalf("replay is %d bytes, so it doesn't test the 6 MB limit", len(content))
	}
	if !bytes.HasSuffix(bytes.TrimSpace(content), []byte("</html>")) {
		t.Fatalf("replay was cut short: %q", content[max(0, len(content)-200):])
	}
	t.Logf("replay streamed %d bytes", len(content))

	// The demo of the same game streams packed with gzip to a browser, which
	// asks for it, and unpacks to the whole file.
	response, content = callWithHeaders(t, invoke, http.MethodGet, "/game/from-lobby/"+lobbyID+"/demo", "", "Bearer "+playerToken,
		map[string]string{"accept-encoding": "gzip, deflate, br, zstd"})
	if response.StatusCode != http.StatusOK || response.Headers["Content-Encoding"] != "gzip" || response.Headers["Vary"] != "Accept-Encoding" {
		t.Fatalf("demo=%d %v", response.StatusCode, response.Headers)
	}
	unpacker, err := gzip.NewReader(bytes.NewReader(content))
	if err != nil {
		t.Fatal(err)
	}
	unpacked, err := io.ReadAll(unpacker)
	if err != nil {
		t.Fatal(err)
	}
	var demo models.Demo
	if err := json.Unmarshal(unpacked, &demo); err != nil {
		t.Fatalf("unpacked demo: %v", err)
	}
	if demo.GameID != binding.GameID || len(demo.Frames) != 141 || len(demo.Logs) != 141 {
		t.Fatalf("demo of game %d has %d frames and %d log entries", demo.GameID, len(demo.Frames), len(demo.Logs))
	}
	t.Logf("demo streamed %d bytes packed, %d unpacked", len(content), len(unpacked))

	// The graph page reads any game without authentication, so the public
	// host doesn't serve it.
	if response, content := call(t, invoke, http.MethodGet, fmt.Sprintf("/game/%d/graph", binding.GameID), "", ""); response.StatusCode != http.StatusNotFound {
		t.Fatalf("graph on the Lambda host=%d %.500s", response.StatusCode, content)
	}
}

// publicRoutes are the only routes the public Lambda host serves without
// authentication. None reads game data. Add a route here only on purpose.
// A route listed without a method allows every method, for static mounts.
var publicRoutes = map[string]string{
	"GET /healthz":                         "readiness",
	"GET /":                                "redirect to the 3D client at /play/",
	"/lobby":                               "410 Gone: the lobby moved to its own origin",
	"/lobby/*":                             "410 Gone: the lobby moved to its own origin",
	"GET /map/templates":                   "lists default map templates, which are not game data",
	"GET /map/templates/{map_template_id}": "serves a listed default map as a map file (#390), which is not game data",
	"GET /game/upgrades":                   "lists the static upgrade cost table, which is not game data",
	"GET /bots/defaults":                   "lists the static default bot catalog, which is not game data",
	"GET /game/bot-compute":                "says whether uploaded bots may start this month, from a 10-minute cache, which is not game data",
	"POST /map/validate":                   "checks a map file and stores nothing",
	"GET /leaderboard":                     "the public leaderboard (#367), which reads competitors and their ratings, not game data",
	"GET /leaderboard/{competitor_id}":     "a competitor's public page (#367), from the rating history, which outlives the games",
	"GET /bots/registered":                 "the public registered bots, with their owners and URLs, which anyone may choose by ID; competitors, not game data",
	"/game/demo/*":                         "embedded static browser demo",
	"GET /map/builder":                     "redirect to the embedded map builder",
	"/map/builder/*":                       "embedded static map builder",
	"GET /game/demo-viewer":                "redirect to the embedded demo viewer",
	"/game/demo-viewer/*":                  "embedded static demo viewer, which reads demo files in the browser",
	"GET /docs":                            "API documentation",
	"GET /openapi.json":                    "API document",
	"GET /openapi.yaml":                    "API document",
	"GET /openapi-3.0.json":                "API document",
	"GET /openapi-3.0.yaml":                "API document",
	"GET /schemas/{schema}":                "API schemas",
}

// signedMounts serve only operations that check a Lobby-Signature: the
// game's hooks. A mount answers a path without an operation with 404, so
// each is checked with its operations' paths.
var signedMounts = map[string][]string{
	"/game/lobby-hooks/*": {
		"POST /game/lobby-hooks/validate-settings", "POST /game/lobby-hooks/validate-seat", "POST /game/lobby-hooks/validate-member-settings",
		"POST /game/lobby-hooks/ready-check", "POST /game/lobby-hooks/start", "POST /game/lobby-hooks/round-ended",
	},
}

// Every other route must refuse a request without credentials: no bearer
// token, no admin key and no Lobby-Signature.
func TestTheLambdaHostServesOnlyListedRoutesWithoutAuthentication(t *testing.T) {
	database := testutils.InitTestDB(t)
	config := testConfig(t, database, startTestLobby(t))
	config.AdminAPIKey = "test-admin-api-key"
	handler, err := newHandler(config, database, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	router, ok := handler.(chi.Routes)
	if !ok {
		t.Fatalf("handler %T is not a chi router", handler)
	}
	invoke := lambdaurl.Wrap(handler)
	parameter := regexp.MustCompile(`\{[^}]+\}`)
	guarded := 0
	if err := chi.Walk(router, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		if _, public := publicRoutes[method+" "+route]; public {
			return nil
		}
		if _, public := publicRoutes[route]; public {
			return nil
		}
		if _, signed := signedMounts[route]; signed {
			return nil
		}
		// {id} is a game's number, such as GET /game/{id}/demo's; the
		// other parameters are UUIDs.
		path := parameter.ReplaceAllStringFunc(route, func(name string) string {
			if name == "{id}" {
				return "1"
			}
			return "00000000-0000-4000-8000-000000000001"
		})
		response, content := call(t, invoke, method, path, `{}`, "")
		if response.StatusCode != http.StatusUnauthorized && response.StatusCode != http.StatusForbidden {
			return fmt.Errorf("%s %s answered %d without credentials; protect it, or list it in publicRoutes if it is meant to be public: %.300s", method, route, response.StatusCode, content)
		}
		guarded++
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if guarded == 0 {
		t.Fatal("found no authenticated routes")
	}
	for mount, operations := range signedMounts {
		if !router.Match(chi.NewRouteContext(), http.MethodPost, strings.TrimSuffix(mount, "*")+"x") {
			t.Fatalf("%s is not served", mount)
		}
		for _, operation := range operations {
			method, path, _ := strings.Cut(operation, " ")
			if response, content := call(t, invoke, method, path, `{}`, ""); response.StatusCode != http.StatusUnauthorized {
				t.Fatalf("%s answered %d without a Lobby-Signature: %.300s", operation, response.StatusCode, content)
			}
		}
	}
	if router.Match(chi.NewRouteContext(), http.MethodGet, "/game/1/graph") {
		t.Fatal("the Lambda host serves the graph page")
	}
	if response, content := call(t, invoke, http.MethodPost, "/map/create", `{}`, ""); response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("map creation without the admin key=%d %s", response.StatusCode, content)
	}
	// The game's bare name opens the 3D client at /play/ (#279).
	if response, content := call(t, invoke, http.MethodGet, "/", "", ""); response.StatusCode != http.StatusTemporaryRedirect || response.Headers["Location"] != "/play/" {
		t.Fatalf("/=%d to %q %s", response.StatusCode, response.Headers["Location"], content)
	}
	// The lobby's old routes are gone from the game's origin.
	for _, path := range []string{"/lobby/v1/sessions", "/lobby/v1/jwks.json", "/lobby/ui/"} {
		if response, content := call(t, invoke, http.MethodGet, path, "", ""); response.StatusCode != http.StatusGone || !strings.Contains(string(content), config.LobbyURL) {
			t.Fatalf("%s=%d %s", path, response.StatusCode, content)
		}
	}

	local, err := app.New(config, database, slog.New(slog.NewTextHandler(io.Discard, nil)), app.WithGameGraph())
	if err != nil {
		t.Fatal(err)
	}
	if !local.(chi.Routes).Match(chi.NewRouteContext(), http.MethodGet, "/game/1/graph") {
		t.Fatal("the local server host's options don't serve the graph page")
	}
}

// enlargeGame adds generations that share one map with many castles. The
// replay is derived from the stored generations and draws every castle once
// per generation, so it passes the buffered response limit.
func enlargeGame(t *testing.T, database *gorm.DB, gameID uint) {
	t.Helper()
	const castleCount, generationCount = 1000, 140
	largeMap := models.Map{MapFields: models.MapFields{MapName: "Large", PlaneSizeX: 1000, PlaneSizeY: 1000}}
	if err := database.Create(&largeMap).Error; err != nil {
		t.Fatal(err)
	}
	castles := make([]models.Castle, 0, castleCount)
	for index := range castleCount {
		castles = append(castles, models.Castle{MapID: largeMap.ID, SoldierAmount: 30, CastleFields: models.CastleFields{
			TeamColor:          uint(index % 3),
			PersistentCastleID: uint(index),
			Position:           pgtype.Point{P: pgtype.Vec2{X: float64(index % 40), Y: float64(index / 40)}, Valid: true},
			Upgrades:           models.Upgrades{},
		}})
	}
	if err := database.CreateInBatches(&castles, 500).Error; err != nil {
		t.Fatal(err)
	}
	generations := make([]models.Generation, 0, generationCount)
	for range generationCount {
		generations = append(generations, models.Generation{GameID: gameID, MapID: largeMap.ID, Clashes: []models.Clash{}})
	}
	if err := database.Omit(clause.Associations).Create(&generations).Error; err != nil {
		t.Fatal(err)
	}
}

// Lambda's log-level filtering reads "level" and an RFC 3339 "timestamp" from
// JSON lines, and counts a line without a valid timestamp as INFO, which its
// WARN filter drops. INFO lines, such as request logs, aren't written at all.
func TestLambdaLogsAreJSONWithTheLevelAndTimestampLambdaReads(t *testing.T) {
	var output bytes.Buffer
	logger, _ := observability.NewLambdaLogger(&output, observability.DefaultSettings())
	logger.Info("Request served", "status", 200)
	logger.Warn("Slow query", "duration_ms", 250)
	var line map[string]any
	if err := json.Unmarshal(output.Bytes(), &line); err != nil {
		t.Fatalf("log output is not one JSON object: %q", output.String())
	}
	if line["level"] != "WARN" || line["msg"] != "Slow query" || line["duration_ms"] != float64(250) {
		t.Fatalf("log line = %v", line)
	}
	timestamp, ok := line["timestamp"].(string)
	if _, err := time.Parse(time.RFC3339Nano, timestamp); !ok || err != nil || line["time"] != nil {
		t.Fatalf("log line has no RFC 3339 timestamp: %v", line)
	}
}

// A backend that can't start answers 503 and tries again at most every
// startRetryInterval, rather than failing the cold start, which Lambda would
// retry with a platform log line on every request.
func TestTheLambdaHostAnswers503UntilTheBackendStarts(t *testing.T) {
	var output bytes.Buffer
	logger, logs := observability.NewLambdaLogger(&output, observability.DefaultSettings())
	now := time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
	attempts := 0
	instance := newHost(logger, logs, func(context.Context) (http.Handler, error) {
		attempts++
		if attempts == 1 {
			return nil, errors.New("database unreachable")
		}
		return http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) { response.WriteHeader(http.StatusNoContent) }), nil
	}, func() time.Time { return now })
	serve := func() int {
		recorder := httptest.NewRecorder()
		instance.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/healthz", nil))
		return recorder.Code
	}
	if status := serve(); status != http.StatusServiceUnavailable || attempts != 1 {
		t.Fatalf("first request=%d after %d attempts", status, attempts)
	}
	if status := serve(); status != http.StatusServiceUnavailable || attempts != 1 {
		t.Fatalf("request within the retry interval=%d after %d attempts", status, attempts)
	}
	if lines := strings.Count(output.String(), "\n"); lines != 1 || !strings.Contains(output.String(), "database unreachable") {
		t.Fatalf("logged %d lines: %s", lines, output.String())
	}
	now = now.Add(startRetryInterval)
	if status := serve(); status != http.StatusNoContent || attempts != 2 {
		t.Fatalf("request after the retry interval=%d after %d attempts", status, attempts)
	}
}

// A request's work ends before Lambda's timeout, which would reset the
// instance and log a platform line outside the instance's limits.
func TestRequestsEndBeforeTheLambdaTimeout(t *testing.T) {
	logger, logs := observability.NewLambdaLogger(io.Discard, observability.DefaultSettings())
	var deadline time.Time
	instance := newHost(logger, logs, func(context.Context) (http.Handler, error) {
		return http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) { deadline, _ = request.Context().Deadline() }), nil
	}, time.Now)
	timeout := time.Now().Add(30 * time.Second)
	ctx, cancel := context.WithDeadline(context.Background(), timeout)
	defer cancel()
	instance.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/healthz", nil).WithContext(ctx))
	if !deadline.Equal(timeout.Add(-deadlineMargin)) {
		t.Fatalf("request deadline = %v, want %v before the invocation's %v", deadline, deadlineMargin, timeout)
	}
}

// A request that fails writes one ERROR line, which the error alarm counts,
// with its operation and the cause that its response leaves out.
func TestAFailedRequestLogsOneErrorLineWithItsCause(t *testing.T) {
	database := testutils.InitTestDB(t)
	lobby := startTestLobby(t)
	var output bytes.Buffer
	logger, _ := observability.NewLambdaLogger(&output, observability.DefaultSettings())
	handler, err := newHandler(testConfig(t, database, lobby), database, logger)
	if err != nil {
		t.Fatal(err)
	}
	invoke := lambdaurl.Wrap(handler)
	// The database goes away.
	sqlDatabase, err := database.DB()
	if err != nil {
		t.Fatal(err)
	}
	if err := sqlDatabase.Close(); err != nil {
		t.Fatal(err)
	}
	playerToken := lobby.playerToken(t, uuid.NewString())
	for _, test := range []struct {
		name, route, operation string
		send                   func(*testing.T) *events.LambdaFunctionURLStreamingResponse
	}{
		{"a public operation", "/map/templates", "listMapTemplates", func(t *testing.T) *events.LambdaFunctionURLStreamingResponse {
			response, _ := call(t, invoke, http.MethodGet, "/map/templates", "", "")
			return response
		}},
		{"an operation with a player token", "/game/from-lobby/{lobby_id}/generation", "getIslandLobbyGeneration", func(t *testing.T) *events.LambdaFunctionURLStreamingResponse {
			response, _ := call(t, invoke, http.MethodGet, "/game/from-lobby/"+uuid.NewString()+"/generation", "", "Bearer "+playerToken)
			return response
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			output.Reset()
			if response := test.send(t); response.StatusCode < http.StatusInternalServerError {
				t.Fatalf("status=%d", response.StatusCode)
			}
			var errorLines []map[string]any
			for _, text := range strings.Split(strings.TrimSpace(output.String()), "\n") {
				var line map[string]any
				if err := json.Unmarshal([]byte(text), &line); err != nil {
					t.Fatalf("log line is not JSON: %q", text)
				}
				// The game retention pass that ends a game request fails too,
				// and logs its own line.
				if line["level"] == "ERROR" && line["msg"] != "Game retention pass failed" {
					errorLines = append(errorLines, line)
				}
			}
			if len(errorLines) != 1 || errorLines[0]["route"] != test.route || !strings.Contains(fmt.Sprint(errorLines[0]["errors"]), "database is closed") ||
				errorLines[0]["operation"] != test.operation {
				t.Fatalf("error lines = %v\nall output: %s", errorLines, output.String())
			}
		})
	}
}

// Settings whose worst case is over the budget stop the backend from
// serving, whatever else is configured.
func TestTheLambdaHostRefusesSettingsOverTheBudget(t *testing.T) {
	_, err := observability.LoadSettings(func(name string) (string, bool) {
		value, ok := map[string]string{observability.ConcurrencyEnv: "50"}[name]
		return value, ok
	})
	if err == nil || !strings.Contains(err.Error(), "over the budget") {
		t.Fatalf("LoadSettings() error = %v, want the budget refused", err)
	}
}
