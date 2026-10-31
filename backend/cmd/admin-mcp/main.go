// Command admin-mcp is a Model Context Protocol server that answers an
// operator's questions about lobbies, players and Island Battles games, such
// as how many lobbies are active or how many players are in a lobby. It runs
// on the operator's own machine, serves one client over stdio, and connects
// straight to PostgreSQL, locally or in production, with
// ISLAND_BATTLES_ADMIN_DATABASE_URL.
//
// It can't change data: its one connection's sessions are read-only, every
// tool runs in a read-only transaction that is rolled back, and production
// gives it a role that may only read. It logs to stderr, because stdout
// carries the MCP stream.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"islandBattles/lobby/adminmcp"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"gorm.io/gorm"
)

// DatabaseURLEnv names the database to answer from. In production it is the
// read-only role's URL over TLS.
const DatabaseURLEnv = "ISLAND_BATTLES_ADMIN_DATABASE_URL"

// instructions tell a client how the tools fit together.
const instructions = "Answers questions about Island Battles lobbies, players and games from its database, and never changes anything. " +
	"overview counts lobbies, connected members and games; list_lobbies and get_lobby describe lobbies; find_players finds players; " +
	"list_games, get_game and get_generation describe games. For anything else, read describe_schema and then run SQL with query. " +
	"Times are UTC. A member is connected while one of its leases has neither expired nor been released."

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	url := os.Getenv(DatabaseURLEnv)
	if url == "" {
		logger.Error(DatabaseURLEnv + " is required: the URL of the database to answer from")
		os.Exit(1)
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if err := run(ctx, url, &mcp.StdioTransport{}, logger); err != nil {
		logger.Error("The admin MCP server stopped", "error", err)
		os.Exit(1)
	}
}

// run serves the tools over transport until the client disconnects or ctx
// ends.
func run(ctx context.Context, url string, transport mcp.Transport, logger *slog.Logger) error {
	database, err := adminmcp.OpenDatabase(ctx, url, logger)
	if err != nil {
		return err
	}
	sqlDatabase, err := database.DB()
	if err != nil {
		return err
	}
	defer func() { _ = sqlDatabase.Close() }()
	logger.Info("Serving the admin MCP server over stdio")
	if err := newServer(database).Run(ctx, transport); err != nil && !errors.Is(err, context.Canceled) {
		return fmt.Errorf("serve MCP: %w", err)
	}
	return nil
}

// newServer registers the lobby tools, the Island Battles game tools and the
// SQL tools.
func newServer(database *gorm.DB) *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{Name: "island-battles-admin", Title: "Island Battles admin", Version: "1.0.0"},
		&mcp.ServerOptions{Instructions: instructions})
	adminmcp.AddTools(server, database, adminmcp.Options{
		Games:         islandBattlesGames{},
		GamesOverview: "Island Battles games running and finished, and the generations created in the last hour, one per turn played",
	})
	addGameTools(server, database)
	addSQLTools(server, database)
	return server
}
