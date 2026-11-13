// Command admin-mcp is a Model Context Protocol server that answers an
// operator's questions about lobbies, players and Island Battles games, such
// as how many lobbies are active or how many players are in a lobby. It runs
// on the operator's own machine, serves one client over stdio, and connects
// straight to PostgreSQL, locally or in production, to two databases (#229):
// Island Battles', ISLAND_BATTLES_ADMIN_DATABASE_URL, and the lobby's,
// ISLAND_BATTLES_ADMIN_LOBBY_DATABASE_URL.
//
// It can't change data: its one connection to each database has read-only
// sessions, every tool runs in a read-only transaction that is rolled back,
// and production gives it a role that may only read. It logs to stderr,
// because stdout carries the MCP stream.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"islandBattles/backend/dependencies"
	"islandBattles/lobby/adminmcp"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"gorm.io/gorm"
)

// DatabaseURLEnv and LobbyDatabaseURLEnv name the databases to answer from:
// Island Battles' and the lobby's. In production they are the read-only
// role's URLs over TLS.
const (
	DatabaseURLEnv      = "ISLAND_BATTLES_ADMIN_DATABASE_URL"
	LobbyDatabaseURLEnv = "ISLAND_BATTLES_ADMIN_LOBBY_DATABASE_URL"
)

// instructions tell a client how the tools fit together.
const instructions = "Answers questions about Island Battles lobbies, players and games from its database, and never changes anything. " +
	"overview counts lobbies, connected members and games; list_lobbies and get_lobby describe lobbies; find_players finds players; " +
	"list_games, get_game and get_generation describe games. For anything else, read describe_schema and then run SQL with query, " +
	"in database game, Island Battles' games, or lobby, the lobby's lobbies and players: they are two databases, so no query joins them. " +
	"Times are UTC. A member is connected while one of its leases has neither expired nor been released."

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	urls := databaseURLs{game: os.Getenv(DatabaseURLEnv), lobby: os.Getenv(LobbyDatabaseURLEnv)}
	if urls.game == "" || urls.lobby == "" {
		logger.Error(DatabaseURLEnv + " and " + LobbyDatabaseURLEnv + " are required: the URLs of Island Battles' database and the lobby's")
		os.Exit(1)
	}
	gameRetentionPeriod, err := dependencies.LoadGameRetentionPeriodFromLookup(os.LookupEnv)
	if err != nil {
		logger.Error("Invalid configuration", "error", err)
		os.Exit(1)
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if err := run(ctx, urls, gameRetentionPeriod, &mcp.StdioTransport{}, logger); err != nil {
		logger.Error("The admin MCP server stopped", "error", err)
		os.Exit(1)
	}
}

// databaseURLs are the URLs of Island Battles' database and the lobby's.
type databaseURLs struct{ game, lobby string }

// run serves the tools over transport until the client disconnects or ctx
// ends.
func run(ctx context.Context, urls databaseURLs, gameRetentionPeriod time.Duration, transport mcp.Transport, logger *slog.Logger) error {
	game, err := open(ctx, "Island Battles' database", urls.game, logger)
	if err != nil {
		return err
	}
	defer closeDatabase(game)
	lobby, err := open(ctx, "the lobby's database", urls.lobby, logger)
	if err != nil {
		return err
	}
	defer closeDatabase(lobby)
	logger.Info("Serving the admin MCP server over stdio")
	if err := newServer(game, lobby, logger, gameRetentionPeriod).Run(ctx, transport); err != nil && !errors.Is(err, context.Canceled) {
		return fmt.Errorf("serve MCP: %w", err)
	}
	return nil
}

// open connects to one of the databases as adminmcp.OpenDatabase does. query
// refuses to run on a database whose check of the bearer hashes fails; the
// other tools never read them.
func open(ctx context.Context, name, url string, logger *slog.Logger) (*gorm.DB, error) {
	database, err := adminmcp.OpenDatabase(ctx, url, logger)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	if err := adminmcp.ReadOnly(ctx, database, adminmcp.CheckCredentialsHidden); err != nil {
		logger.Error("query is refused in "+name, "error", credentialsError(err))
	}
	return database, nil
}

func closeDatabase(database *gorm.DB) {
	if sqlDatabase, err := database.DB(); err == nil {
		_ = sqlDatabase.Close()
	}
}

// newServer registers the lobby tools on the lobby's database, the Island
// Battles game tools on Island Battles', and the SQL tools on both.
// gameRetentionPeriod is how long the backend keeps a game after its latest
// generation.
func newServer(game, lobby *gorm.DB, logger *slog.Logger, gameRetentionPeriod time.Duration) *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{Name: "island-battles-admin", Title: "Island Battles admin", Version: "1.0.0"},
		&mcp.ServerOptions{Instructions: instructions})
	adminmcp.AddTools(server, lobby, adminmcp.Options{
		Games:         islandBattlesGames{database: game},
		GamesOverview: "Island Battles games running and finished, and the generations created in the last hour, one per turn played",
	})
	addGameTools(server, game, gameRetentionPeriod)
	addSQLTools(server, sqlDatabases{GameDatabase: game, LobbyDatabase: lobby}, logger)
	return server
}
