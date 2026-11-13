// Command lambda serves the HTTP API from AWS Lambda behind a Function URL.
// The Function URL must use the RESPONSE_STREAM invoke mode, which lambdaurl
// requires and which lets replay pages exceed Lambda's 6 MB limit
// on buffered responses. Build it for the provided.al2023 runtime on arm64:
//
//	GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -tags lambda.norpc -trimpath -ldflags='-s -w' -o bootstrap ./backend/cmd/lambda
//
// The runtime looks for an executable named bootstrap at the root of the zip.
//
// It reads the same environment as the HTTP server host, except for
// ISLAND_BATTLES_HTTP_ADDR, and runs no schema changes: run the migrate
// command in backend/cmd/migrate before deploying a new build. It also reads
// the limits that keep its logs and traces inside AWS's free tier
// (backend/observability), and refuses to serve when their worst case is
// over the budget.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"sync"
	"time"

	"islandBattles/backend/app"
	"islandBattles/backend/dependencies"
	"islandBattles/backend/observability"

	"github.com/aws/aws-lambda-go/lambdaurl"
	"gorm.io/gorm"
)

// serviceName names the backend's traces in X-Ray.
const serviceName = "island-battles-backend"

// startRetryInterval spaces out attempts to start after a failed one.
const startRetryInterval = 10 * time.Second

// deadlineMargin ends a request's work before Lambda's timeout does. A
// timeout resets the instance and logs a platform line that the instance's
// limits don't cover.
const deadlineMargin = 2 * time.Second

func main() {
	settings, settingsError := observability.LoadSettings(os.LookupEnv)
	limits := settings
	if settingsError != nil {
		// Logs stay limited while the instance refuses to serve.
		limits = observability.DefaultSettings()
	}
	logger, logs := observability.NewLambdaLogger(os.Stdout, limits)
	slog.SetDefault(logger)
	instance := newHost(logger, logs, func(ctx context.Context) (http.Handler, error) {
		if settingsError != nil {
			return nil, settingsError
		}
		return build(ctx, settings, logger)
	}, time.Now)
	// The first attempt runs during the cold start.
	instance.started(context.Background())
	lambdaurl.Start(instance)
}

// build runs once per cold start. The pools, Island Battles' and the lobby's,
// stay open for the instance's lifetime, so warm invocations reuse their
// connections. A failed attempt closes them, so the retries don't pile up
// connections.
func build(ctx context.Context, settings observability.Settings, logger *slog.Logger) (_ http.Handler, err error) {
	config, err := dependencies.LoadConfig()
	if err != nil {
		return nil, err
	}
	database, err := dependencies.InitDB(config.Database, logger)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err == nil {
			return
		}
		if sqlDatabase, poolError := database.DB(); poolError == nil {
			_ = sqlDatabase.Close()
		}
	}()
	// The lobby's own database, while the lobby runs in this process.
	lobbyDatabase, err := dependencies.InitLobbyDB(config, logger)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err == nil || lobbyDatabase == nil {
			return
		}
		if lobbyPool, poolError := lobbyDatabase.DB(); poolError == nil {
			_ = lobbyPool.Close()
		}
	}()
	handler, err := newHandler(config, database, lobbyDatabase, logger)
	if err != nil {
		return nil, err
	}
	tracing, err := observability.StartTracing(ctx, os.LookupEnv, serviceName, observability.NewIntervalSampler(settings.TraceInterval, time.Now))
	if err != nil {
		return nil, fmt.Errorf("start tracing: %w", err)
	}
	return tracing.Handler(handler), nil
}

// newHandler passes no app options, so the public host leaves out the pages
// that read games without authentication, such as GET /game/{id}/graph.
func newHandler(config dependencies.Config, database, lobbyDatabase *gorm.DB, logger *slog.Logger) (http.Handler, error) {
	return app.New(config, database, lobbyDatabase, logger)
}

// host serves the backend once it has started. Until then it answers 503 and
// tries to start again at most every startRetryInterval, logging each failure
// within the instance's limits. Failing the cold start instead would make
// Lambda retry it, with a platform log line, on every request.
type host struct {
	logger  *slog.Logger
	logs    *observability.LimitedHandler
	start   func(context.Context) (http.Handler, error)
	now     func() time.Time
	mutex   sync.Mutex
	handler http.Handler
	tried   time.Time
}

func newHost(logger *slog.Logger, logs *observability.LimitedHandler, start func(context.Context) (http.Handler, error), now func() time.Time) *host {
	return &host{logger: logger, logs: logs, start: start, now: now}
}

func (instance *host) ServeHTTP(response http.ResponseWriter, request *http.Request) {
	defer func() { _ = instance.logs.Flush() }()
	handler := instance.started(request.Context())
	if handler == nil {
		http.Error(response, "the backend failed to start", http.StatusServiceUnavailable)
		return
	}
	if deadline, ok := request.Context().Deadline(); ok {
		ctx, cancel := context.WithDeadline(request.Context(), deadline.Add(-deadlineMargin))
		defer cancel()
		request = request.WithContext(ctx)
	}
	handler.ServeHTTP(response, request)
}

// started returns the backend's handler, starting it first if it is time to
// try again, or nil.
func (instance *host) started(ctx context.Context) http.Handler {
	instance.mutex.Lock()
	defer instance.mutex.Unlock()
	if instance.handler != nil {
		return instance.handler
	}
	now := instance.now()
	if !instance.tried.IsZero() && now.Sub(instance.tried) < startRetryInterval {
		return nil
	}
	instance.tried = now
	handler, err := instance.start(ctx)
	if err != nil {
		instance.logger.ErrorContext(ctx, "Backend failed to start; it answers 503 until it does", "error", err, "retry_after", startRetryInterval.String())
		return nil
	}
	instance.handler = handler
	return handler
}
