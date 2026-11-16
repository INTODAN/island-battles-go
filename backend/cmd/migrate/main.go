// Command migrate brings Island Battles' database to the schema this build
// expects: the map template migrations, then the game tables. Run it once per
// deploy, and locally before starting the backend; the hosts of the HTTP API
// only connect. The lobby runs on its own, with its own database and its own
// migrations (github.com/INTODAN/mpl).
//
// It reads ISLAND_BATTLES_DATABASE_URL and the optional pool settings, and
// nothing else, so a deploy script can run it with the database credentials
// alone.
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"islandBattles/backend/dependencies"
	"islandBattles/backend/schema"

	"github.com/lmittmann/tint"
)

func main() {
	logger := slog.New(tint.NewHandler(os.Stderr, &tint.Options{Level: slog.LevelInfo}))
	config, err := dependencies.LoadDatabaseConfig()
	if err != nil {
		logger.Error("Invalid configuration", "error", err)
		os.Exit(1)
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if err := run(ctx, config, logger); err != nil {
		logger.Error("Migration failed", "error", err)
		os.Exit(1)
	}
}

// run migrates Island Battles' database.
func run(ctx context.Context, config dependencies.DatabaseConfig, logger *slog.Logger) error {
	database, err := dependencies.InitDB(config, logger)
	if err != nil {
		return err
	}
	sqlDatabase, err := database.DB()
	if err != nil {
		return err
	}
	defer func() { _ = sqlDatabase.Close() }()
	if err := schema.Migrate(ctx, database); err != nil {
		return err
	}
	logger.Info("Database schema is up to date")
	return nil
}
