// Command backend serves the HTTP API with net/http, for local development.
// Run the migrate command in backend/cmd/migrate first. The AWS Lambda host
// of the same API is in backend/cmd/lambda.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"islandBattles/backend/app"
	"islandBattles/backend/botrunner"
	"islandBattles/backend/dependencies"
	"islandBattles/backend/observability"
	"islandBattles/backend/packagestore/locals3"

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

// localOrigin is where players and this host itself reach this host: the
// public URL when one is set, and otherwise localhost on the port it listens
// on. Presigned uploads of bot packages go there.
func localOrigin(config dependencies.Config, address string) string {
	if config.PublicURL != "" {
		return config.PublicURL
	}
	host, port, err := net.SplitHostPort(address)
	if err != nil || host == "" || host == "0.0.0.0" || host == "::" {
		host = "localhost"
	}
	return "http://" + net.JoinHostPort(host, port)
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
	options := []app.Option{app.WithGameGraph()}
	if config.BotPackagesBucket == "" {
		// Without a bucket, bot packages are kept on disk, behind a fake S3
		// that this host serves itself.
		directory := dependencies.LoadBotPackagesDir(os.LookupEnv)
		store, storage, err := locals3.New(directory, localOrigin(config, address))
		if err != nil {
			return err
		}
		options = append(options, app.WithLocalBotPackages(store, locals3.Prefix, storage))
		logger.Info("Keeping uploaded bot packages on disk", "directory", directory)
	}
	// Without runner functions, the runners play hosted and uploaded bots
	// in this process (#225).
	if config.ServerBots && config.BotRunnerPython == "" {
		origin := localOrigin(config, address)
		parsed, err := url.Parse(origin)
		if err != nil {
			return err
		}
		directory, err := os.MkdirTemp("", "island-battles-bot-runners-")
		if err != nil {
			return err
		}
		runners := &botrunner.Local{SubmitHost: parsed.Host, Python: dependencies.LoadLocalBotPython(os.LookupEnv), Directory: directory, Logger: logger}
		defer runners.Close()
		options = append(options, app.WithLocalBotRunners(runners, origin))
		logger.Info("Playing hosted and uploaded bots with the local bot runners", "python", strings.Join(runners.Python, " "))
	}
	handler, err := app.New(config, database, logger, options...)
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
