package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"testing"

	"islandBattles/backend/app"
	"islandBattles/backend/dependencies"
	"islandBattles/backend/internal/testutils"
	"islandBattles/backend/models"
	"islandBattles/backend/retention"
	"islandBattles/backend/services"
	"islandBattles/lobby/domain"

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

func testConfig(t *testing.T, database *gorm.DB) dependencies.Config {
	t.Helper()
	dialector, ok := database.Dialector.(*gormpostgres.Dialector)
	if !ok {
		t.Fatalf("unexpected dialector %T", database.Dialector)
	}
	// Nothing in this test pushes to Centrifugo, so its address is unused.
	return dependencies.Config{
		Database:                  dependencies.DefaultDatabaseConfig(dialector.DSN),
		CentrifugoPublicURL:       "ws://127.0.0.1:9/connection/websocket",
		CentrifugoAPIURL:          "http://127.0.0.1:9/api",
		CentrifugoAPIKey:          "test-centrifugo-api-key",
		ConnectionTokenHMACSecret: "test-connection-token-secret-of-sufficient-length",
		LobbyProxySecret:          "test-lobby-proxy-secret",
		GameRetentionPeriod:       retention.DefaultPeriod,
	}
}

func TestReplayOverSixMegabytesStreamsThroughTheFunctionURL(t *testing.T) {
	database := testutils.InitTestDB(t)
	handler, err := newHandler(testConfig(t, database), database, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	invoke := lambdaurl.Wrap(handler)

	response, content := call(t, invoke, http.MethodPost, "/lobby/v1/sessions", `{"display_name":"Alice"}`, "")
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("create session=%d %s", response.StatusCode, content)
	}
	var alice domain.GuestSession
	if err := json.Unmarshal(content, &alice); err != nil {
		t.Fatal(err)
	}
	lobbyID, red, blue := uuid.NewString(), uuid.NewString(), uuid.NewString()
	binding, err := services.RunLobbyGame(context.Background(), database, &testutils.RecordedPublisher{}, domain.Snapshot{
		Lobby:       domain.Lobby{ID: lobbyID, GameKey: "island-battles", Phase: domain.PhaseStarted},
		Teams:       []domain.Team{{ID: red, Name: "Red", Order: 1}, {ID: blue, Name: "Blue", Order: 2}},
		StartRoster: domain.StartRoster{{PlayerID: alice.PlayerID, TeamID: red, TeamOrder: 1}, {PlayerID: uuid.NewString(), TeamID: blue, TeamOrder: 2}},
	})
	if err != nil {
		t.Fatal(err)
	}
	enlargeGame(t, database, binding.GameID)

	response, content = call(t, invoke, http.MethodGet, "/game/from-lobby/"+lobbyID+"/replay", "", "Bearer "+alice.AccessToken)
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
	"GET /healthz":            "readiness",
	"POST /lobby/v1/sessions": "creates a guest credential",
	"GET /map/templates":      "lists default map templates, which are not game data",
	"POST /map/validate":      "checks a map file and stores nothing",
	"GET /lobby/ui":           "redirect to the embedded lobby UI",
	"/lobby/ui/*":             "embedded static lobby UI",
	"/game/demo/*":            "embedded static browser demo",
	"GET /map/builder":        "redirect to the embedded map builder",
	"/map/builder/*":          "embedded static map builder",
	"GET /docs":               "API documentation",
	"GET /openapi.json":       "API document",
	"GET /openapi.yaml":       "API document",
	"GET /openapi-3.0.json":   "API document",
	"GET /openapi-3.0.yaml":   "API document",
	"GET /schemas/{schema}":   "API schemas",
}

// Every other route must refuse a request without credentials: no bearer
// token, no admin key and no proxy secret.
func TestTheLambdaHostServesOnlyListedRoutesWithoutAuthentication(t *testing.T) {
	database := testutils.InitTestDB(t)
	config := testConfig(t, database)
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
		path := parameter.ReplaceAllString(route, "00000000-0000-4000-8000-000000000001")
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
	if router.Match(chi.NewRouteContext(), http.MethodGet, "/game/1/graph") {
		t.Fatal("the Lambda host serves the graph page")
	}
	if response, content := call(t, invoke, http.MethodPost, "/map/create", `{}`, ""); response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("map creation without the admin key=%d %s", response.StatusCode, content)
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

// The log handler must keep slog's default "level" key, because Lambda's
// log-level filtering reads it from JSON lines.
func TestLambdaLogsAreJSONWithTheDefaultLevelKey(t *testing.T) {
	var output bytes.Buffer
	newLogger(&output).Info("Request served", "status", 200)
	newLogger(&output).Debug("Not written")
	var line map[string]any
	if err := json.Unmarshal(output.Bytes(), &line); err != nil {
		t.Fatalf("log line is not one JSON object: %q", output.String())
	}
	if line["level"] != "INFO" || line["msg"] != "Request served" || line["status"] != float64(200) {
		t.Fatalf("log line = %v", line)
	}
}
