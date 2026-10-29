// Command backend serves the HTTP API with net/http, for local development.
// Run the migrate command in backend/cmd/migrate first. The AWS Lambda host
// of the same API is in backend/cmd/lambda.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"islandBattles/backend/app"
	"islandBattles/backend/dependencies"
	"islandBattles/backend/observability"

	"github.com/lmittmann/tint"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

func main() {
	logger := slog.New(observability.WithTraceIDs(tint.NewHandler(os.Stdout, &tint.Options{Level: slog.LevelInfo})))
	slog.SetDefault(logger)
	config, err := dependencies.LoadConfig()
	if err != nil {
		logger.Error("Invalid configuration", "error", err)
		os.Exit(1)
	}
	address, err := dependencies.LoadHTTPAddr()
	if err != nil {
		logger.Error("Invalid configuration", "error", err)
		os.Exit(1)
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if err := run(ctx, config, address, logger); err != nil {
		logger.Error("Backend stopped", "error", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, config dependencies.Config, address string, logger *slog.Logger) error {
	// Locally, tracing is off unless OTEL_EXPORTER_OTLP_ENDPOINT is set, and
	// then records every request. The free-tier limits only apply on Lambda.
	tracing, err := observability.StartTracing(ctx, os.LookupEnv, "island-battles-backend", sdktrace.AlwaysSample())
	if err != nil {
		return fmt.Errorf("start tracing: %w", err)
	}
	defer func() { _ = tracing.Shutdown(context.Background()) }()
	database, err := dependencies.InitDB(config.Database, logger)
	if err != nil {
		return err
	}
	sqlDatabase, err := database.DB()
	if err != nil {
		return err
	}
	defer func() { _ = sqlDatabase.Close() }()
	// The graph page reads any game without authentication, so only this local
	// host serves it.
	handler, err := app.New(config, database, logger, app.WithGameGraph())
	if err != nil {
		return err
	}
	server := &http.Server{Addr: address, Handler: tracing.Handler(handler), ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 60 * time.Second}
	serverContext, stopServer := context.WithCancel(ctx)
	defer stopServer()
	shutdownDone := make(chan struct{})
	go func() {
		defer close(shutdownDone)
		<-serverContext.Done()
		shutdownContext, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownContext); err != nil {
			logger.Error("HTTP shutdown timed out", "error", err)
			_ = server.Close()
		}
	}()
	logger.Info("Listening", "address", address)
	serveError := server.ListenAndServe()
	stopServer()
	<-shutdownDone
	if serveError != nil && !errors.Is(serveError, http.ErrServerClosed) {
		return fmt.Errorf("serve HTTP: %w", serveError)
	}
	return nil
}
