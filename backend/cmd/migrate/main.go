// Command migrate brings Island Battles' database to the schema this build
// expects: the map template migrations, then the game tables. Run it once per
// deploy, and locally before starting the backend; the hosts of the HTTP API
// only connect. The lobby's schema is in the lobby's own database, which the
// lobby's migrate command, github.com/INTODAN/mpl/cmd/migrate, migrates first.
//
// It reads ISLAND_BATTLES_DATABASE_URL and the optional pool settings, and
// LOBBY_DATABASE_URL, and nothing else, so a deploy script can run it with
// the database credentials alone. With LOBBY_DATABASE_URL, it then drops the
// lobby's tables that builds before #229 left in Island Battles' database,
// but only when both servers say the two are different databases and the
// lobby's database has this build's lobby migrations (schema.CheckLobbyTables).
// Otherwise they stay: until the lobby's own database is ready, they are the
// only ones it has.
package main

import (
	"context"
	"fmt"
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
	lobbyURL := strings.TrimSpace(os.Getenv(dependencies.LobbyDatabaseURLEnv))
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if err := run(ctx, config, lobbyURL, logger); err != nil {
		logger.Error("Migration failed", "error", err)
		os.Exit(1)
	}
}

// run migrates Island Battles' database, and with lobbyURL, the lobby's own
// database, drops the lobby's tables from it if schema.CheckLobbyTables
// allows it.
func run(ctx context.Context, config dependencies.DatabaseConfig, lobbyURL string, logger *slog.Logger) error {
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
	if lobbyURL == "" {
		logger.Info("Database schema is up to date. The lobby's tables, if any, stay: " + dependencies.LobbyDatabaseURLEnv + " is not set")
		return nil
	}
	lobby, err := dependencies.InitDB(dependencies.DefaultDatabaseConfig(lobbyURL), logger)
	if err != nil {
		return fmt.Errorf("the lobby's database: %w", err)
	}
	if lobbyPool, err := lobby.DB(); err == nil {
		defer func() { _ = lobbyPool.Close() }()
	}
	check, err := schema.CheckLobbyTables(ctx, database, lobby)
	if err != nil {
		return err
	}
	if !check.Drop {
		logger.Warn("Database schema is up to date, but the lobby's tables in it were not dropped: " + check.Reason)
		return nil
	}
	if err := schema.DropLobbyTables(ctx, database); err != nil {
		return err
	}
	logger.Info("Database schema is up to date, without the lobby's tables: " + check.Reason)
	return nil
}
