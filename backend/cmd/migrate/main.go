// Command migrate brings Island Battles' database to the schema this build
// expects: the map template migrations, then the game tables. Run it once per
// deploy, and locally before starting the backend; the hosts of the HTTP API
// only connect. The lobby's schema is in the lobby's own database, which the
// lobby's migrate command, lobby/cmd/migrate, migrates.
//
// It reads ISLAND_BATTLES_DATABASE_URL and the optional pool settings, and
// LOBBY_DATABASE_URL, and nothing else, so a deploy script can run it with
// the database credentials alone. When LOBBY_DATABASE_URL names another
// database, the lobby keeps its tables there, so the command drops the copies
// that builds before it left in Island Battles' database (#229). Without it,
// or with the same database, they stay: until the lobby has a database of its
// own, they are the only ones it has.
package main

import (
	"context"
	"islandBattles/backend/schema"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"islandBattles/backend/dependencies"

	"github.com/lmittmann/tint"
)

func main() {
	logger := slog.New(tint.NewHandler(os.Stderr, &tint.Options{Level: slog.LevelInfo}))
	config, err := dependencies.LoadDatabaseConfig()
	if err != nil {
		logger.Error("Invalid configuration", "error", err)
		os.Exit(1)
	}
	options, err := loadOptions(config, os.LookupEnv)
	if err != nil {
		logger.Error("Invalid configuration", "error", err)
		os.Exit(1)
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if err := run(ctx, config, options, logger); err != nil {
		logger.Error("Migration failed", "error", err)
		os.Exit(1)
	}
}

// loadOptions reads LOBBY_DATABASE_URL: whether the lobby has a database of
// its own.
func loadOptions(config dependencies.DatabaseConfig, lookup func(string) (string, bool)) (schema.Options, error) {
	lobbyURL, _ := lookup(dependencies.LobbyDatabaseURLEnv)
	separate, err := schema.SeparateLobbyDatabase(config.URL, strings.TrimSpace(lobbyURL))
	if err != nil {
		return schema.Options{}, err
	}
	return schema.Options{SeparateLobbyDatabase: separate}, nil
}

func run(ctx context.Context, config dependencies.DatabaseConfig, options schema.Options, logger *slog.Logger) error {
	database, err := dependencies.InitDB(config, logger)
	if err != nil {
		return err
	}
	sqlDatabase, err := database.DB()
	if err != nil {
		return err
	}
	defer func() { _ = sqlDatabase.Close() }()
	if err := schema.Migrate(ctx, database, options); err != nil {
		return err
	}
	if options.SeparateLobbyDatabase {
		logger.Info("Database schema is up to date, without the lobby's tables, which are in the lobby's own database")
	} else {
		logger.Info("Database schema is up to date. The lobby's tables, if any, stay: " + dependencies.LobbyDatabaseURLEnv + " names no other database")
	}
	return nil
}
