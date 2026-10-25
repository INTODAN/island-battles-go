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
// command in backend/cmd/migrate before deploying a new build.
package main

import (
	"io"
	"log/slog"
	"net/http"
	"os"

	"islandBattles/backend/app"
	"islandBattles/backend/dependencies"

	"github.com/aws/aws-lambda-go/lambdaurl"
	"gorm.io/gorm"
)

func main() {
	logger := newLogger(os.Stdout)
	slog.SetDefault(logger)
	handler, err := build(logger)
	if err != nil {
		logger.Error("Backend failed to start", "error", err)
		os.Exit(1)
	}
	lambdaurl.Start(handler)
}

// newLogger writes JSON lines that CloudWatch Logs Insights can query. It
// keeps slog's default "level" key, which Lambda's log-level filtering reads.
func newLogger(writer io.Writer) *slog.Logger {
	return slog.New(slog.NewJSONHandler(writer, &slog.HandlerOptions{Level: slog.LevelInfo}))
}

// build runs once per cold start. The pool stays open for the instance's
// lifetime, so warm invocations reuse its connections.
func build(logger *slog.Logger) (http.Handler, error) {
	config, err := dependencies.LoadConfig()
	if err != nil {
		return nil, err
	}
	database, err := dependencies.InitDB(config.Database, logger)
	if err != nil {
		return nil, err
	}
	return newHandler(config, database, logger)
}

// newHandler passes no app options, so the public host leaves out the pages
// that read games without authentication, such as GET /game/{id}/graph.
func newHandler(config dependencies.Config, database *gorm.DB, logger *slog.Logger) (http.Handler, error) {
	return app.New(config, database, logger)
}
