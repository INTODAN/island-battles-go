package observability

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"
	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace/noop"
	"go.uber.org/goleak"
)

// causedProblem is a problem that keeps its cause out of the response, like
// the lobby's.
type causedProblem struct{ cause error }

func (problem *causedProblem) Error() string  { return "internal server error" }
func (problem *causedProblem) Unwrap() error  { return problem.cause }
func (problem *causedProblem) GetStatus() int { return http.StatusInternalServerError }

func newTestRouter(logger *slog.Logger) http.Handler {
	router := chi.NewMux()
	router.Use(Requests(logger))
	config := huma.DefaultConfig("test", "1.0.0")
	config.Transformers = append(config.Transformers, RecordProblems)
	api := humachi.New(router, config)
	api.UseMiddleware(NameOperations)
	huma.Register(api, huma.Operation{OperationID: "startLobby", Method: http.MethodPost, Path: "/lobbies/{lobby_id}/start"}, func(ctx context.Context, input *struct {
		LobbyID string `path:"lobby_id"`
	}) (*struct{}, error) {
		Annotate(ctx, slog.String("player_id", "player-1"))
		RecordError(ctx, fmt.Errorf("start game: %w", errors.New("connection refused")))
		return nil, huma.Error503ServiceUnavailable("game is temporarily unavailable")
	})
	huma.Register(api, huma.Operation{OperationID: "joinLobby", Method: http.MethodPost, Path: "/lobbies/{lobby_id}/join"}, func(context.Context, *struct {
		LobbyID string `path:"lobby_id"`
	}) (*struct{}, error) {
		return nil, &causedProblem{cause: fmt.Errorf("lock lobby: %w", errors.New("deadlock detected"))}
	})
	huma.Register(api, huma.Operation{OperationID: "readLobby", Method: http.MethodGet, Path: "/lobbies/{lobby_id}"}, func(context.Context, *struct {
		LobbyID string `path:"lobby_id"`
	}) (*struct{}, error) {
		return nil, huma.Error404NotFound("lobby not found")
	})
	router.Get("/panic", func(http.ResponseWriter, *http.Request) {
		panic(errors.New("assignment to entry in nil map"))
	})
	return router
}

func serve(handler http.Handler, method, path string) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(method, path, nil))
	return recorder
}

// A 5xx is logged once, at ERROR, with its operation, route, error chain and
// IDs, even when the response leaves the cause out.
func TestEvery5xxIsLoggedOnceWithItsErrorChainAndIDs(t *testing.T) {
	var output bytes.Buffer
	router := newTestRouter(slog.New(slog.NewJSONHandler(&output, &slog.HandlerOptions{Level: slog.LevelWarn})))

	if response := serve(router, http.MethodPost, "/lobbies/lobby-1/start"); response.Code != http.StatusServiceUnavailable {
		t.Fatalf("start=%d", response.Code)
	}
	written := lines(t, &output)
	if len(written) != 1 {
		t.Fatalf("logged %d lines: %v", len(written), written)
	}
	line := written[0]
	if line["level"] != "ERROR" || line["operation"] != "startLobby" || line["route"] != "/lobbies/{lobby_id}/start" || line["status"] != float64(503) ||
		line["lobby_id"] != "lobby-1" || line["player_id"] != "player-1" {
		t.Fatalf("error line = %v", line)
	}
	chain := fmt.Sprint(line["errors"])
	if !strings.Contains(chain, "start game: connection refused") || !strings.Contains(chain, "game is temporarily unavailable") {
		t.Fatalf("error chain = %s", chain)
	}

	if response := serve(router, http.MethodPost, "/lobbies/lobby-2/join"); response.Code != http.StatusInternalServerError || strings.Contains(response.Body.String(), "deadlock") {
		t.Fatalf("join=%d %s", response.Code, response.Body.String())
	}
	written = lines(t, &output)
	if len(written) != 1 || !strings.Contains(fmt.Sprint(written[0]["errors"]), "internal server error: lock lobby: deadlock detected") {
		t.Fatalf("problem's cause wasn't logged: %v", written)
	}
}

// Client errors are INFO, so a WARN filter drops them and no caller can fill
// an instance's log limit with them.
func TestClientErrorsAreInfo(t *testing.T) {
	var output bytes.Buffer
	router := newTestRouter(slog.New(slog.NewJSONHandler(&output, nil)))
	if response := serve(router, http.MethodGet, "/lobbies/lobby-1"); response.Code != http.StatusNotFound {
		t.Fatalf("read=%d", response.Code)
	}
	written := lines(t, &output)
	if len(written) != 1 || written[0]["level"] != "INFO" || written[0]["status"] != float64(404) {
		t.Fatalf("lines = %v", written)
	}
}

// A panic answers 500 and is logged once with its value and stack, and the
// process keeps serving: on Lambda, an unrecovered panic in lambdaurl's
// handler goroutine would end the instance.
func TestPanicsAreRecoveredAndLoggedOnce(t *testing.T) {
	var output bytes.Buffer
	router := newTestRouter(slog.New(slog.NewJSONHandler(&output, &slog.HandlerOptions{Level: slog.LevelWarn})))
	if response := serve(router, http.MethodGet, "/panic"); response.Code != http.StatusInternalServerError {
		t.Fatalf("panic=%d", response.Code)
	}
	written := lines(t, &output)
	if len(written) != 1 || written[0]["msg"] != "Request panicked" || !strings.Contains(fmt.Sprint(written[0]["panic"]), "nil map") ||
		!strings.Contains(fmt.Sprint(written[0]["stack"]), "requests_test.go") {
		t.Fatalf("lines = %v", written)
	}
	if response := serve(router, http.MethodGet, "/lobbies/lobby-1"); response.Code != http.StatusNotFound {
		t.Fatalf("after the panic=%d", response.Code)
	}
}

// otlpCollector stands in for the collector layer and keeps what it receives.
type otlpCollector struct {
	mutex    sync.Mutex
	payloads [][]byte
}

func (collector *otlpCollector) ServeHTTP(response http.ResponseWriter, request *http.Request) {
	body, _ := io.ReadAll(request.Body)
	collector.mutex.Lock()
	collector.payloads = append(collector.payloads, body)
	collector.mutex.Unlock()
	response.Header().Set("Content-Type", "application/x-protobuf")
}

func (collector *otlpCollector) received() []byte {
	collector.mutex.Lock()
	defer collector.mutex.Unlock()
	return bytes.Join(collector.payloads, nil)
}

// Nothing runs between requests (#28): a traced request exports its spans
// itself and leaves no goroutine behind, no batch processor, timer or
// kept-alive connection to the collector, which a frozen Lambda instance
// would stop mid-export.
func TestATracedRequestLeavesNoGoroutineBehind(t *testing.T) {
	collector := &otlpCollector{}
	collectorServer := httptest.NewServer(collector)
	defer collectorServer.Close()
	t.Setenv(EndpointEnv, collectorServer.URL)
	t.Cleanup(func() { otel.SetTracerProvider(noop.NewTracerProvider()) })
	// The test server's own goroutines were there before tracing started.
	ignore := goleak.IgnoreCurrent()
	lookup := func(name string) (string, bool) { return collectorServer.URL, name == EndpointEnv }
	tracing, err := StartTracing(context.Background(), lookup, "test", sdktrace.AlwaysSample())
	if err != nil {
		t.Fatal(err)
	}
	router := chi.NewMux()
	router.Use(Requests(slog.New(slog.DiscardHandler)))
	router.Get("/lobbies/{lobby_id}", func(response http.ResponseWriter, request *http.Request) {
		_, query := otel.Tracer("test").Start(request.Context(), "query")
		query.End()
		response.WriteHeader(http.StatusNoContent)
	})
	for range 3 {
		if response := serve(tracing.Handler(router), http.MethodGet, "/lobbies/lobby-1"); response.Code != http.StatusNoContent {
			t.Fatalf("read=%d", response.Code)
		}
	}
	if !bytes.Contains(collector.received(), []byte("GET /lobbies/{lobby_id}")) {
		t.Fatal("the collector didn't receive the request's span")
	}
	if err := goleak.Find(ignore); err != nil {
		t.Fatalf("goroutines outlived the requests: %v", err)
	}
	if err := tracing.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
}

// A sampled request's spans, its own and its Centrifugo call's, reach the
// collector before the request returns, and its error line carries the trace
// ID that finds them.
func TestASampledRequestExportsItsSpansAndLogsItsTraceID(t *testing.T) {
	collector := &otlpCollector{}
	collectorServer := httptest.NewServer(collector)
	defer collectorServer.Close()
	centrifugo := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer centrifugo.Close()
	t.Setenv(EndpointEnv, collectorServer.URL)
	t.Cleanup(func() { otel.SetTracerProvider(noop.NewTracerProvider()) })
	lookup := func(name string) (string, bool) {
		if name == EndpointEnv {
			return collectorServer.URL, true
		}
		return "", false
	}
	tracing, err := StartTracing(context.Background(), lookup, "test", sdktrace.AlwaysSample())
	if err != nil || !tracing.Enabled() {
		t.Fatalf("StartTracing() = %v, %v", tracing.Enabled(), err)
	}
	defer func() { _ = tracing.Shutdown(context.Background()) }()

	var output bytes.Buffer
	logger := slog.New(WithTraceIDs(slog.NewJSONHandler(&output, &slog.HandlerOptions{Level: slog.LevelWarn})))
	client := &http.Client{Transport: HTTPTransport(http.DefaultTransport, "Centrifugo API")}
	router := chi.NewMux()
	router.Use(Requests(logger))
	router.Post("/lobbies/{lobby_id}/start", func(response http.ResponseWriter, request *http.Request) {
		call, _ := http.NewRequestWithContext(request.Context(), http.MethodPost, centrifugo.URL+"/api", nil)
		if published, err := client.Do(call); err == nil {
			_ = published.Body.Close()
		}
		http.Error(response, "unavailable", http.StatusServiceUnavailable)
	})

	if response := serve(tracing.Handler(router), http.MethodPost, "/lobbies/lobby-1/start"); response.Code != http.StatusServiceUnavailable {
		t.Fatalf("start=%d", response.Code)
	}
	written := lines(t, &output)
	if len(written) != 1 || written[0][TraceIDKey] == nil || written[0][XRayTraceIDKey] == nil {
		t.Fatalf("error line has no trace ID: %v", written)
	}
	// The OTLP payload is protobuf, which holds span names as plain bytes and
	// trace IDs as raw ones.
	traceID, err := hex.DecodeString(written[0][TraceIDKey].(string))
	if err != nil {
		t.Fatal(err)
	}
	exported := collector.received()
	for _, want := range [][]byte{[]byte("POST /lobbies/{lobby_id}/start"), []byte("Centrifugo API POST"), traceID} {
		if !bytes.Contains(exported, want) {
			t.Fatalf("the collector didn't receive %q before the request returned", want)
		}
	}
}
