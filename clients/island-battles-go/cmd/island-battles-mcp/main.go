// Command island-battles-mcp is a Model Context Protocol server that a player
// runs on their own computer, so that their AI agent can investigate an
// Island Battles game with them: on turn 40, "how did my castle get
// captured?". It serves one client over stdio and reads games two ways:
//
//   - a demo file, saved with Save demo in a game's replay, which holds every
//     turn's board and every team's bot logs;
//   - a lobby's game by the lobby's join code, live or finished, which it
//     reads from the game's public watcher endpoints like any watcher: a
//     guest session on the lobby, a watch with the code, and the game's
//     history with every team's bot logs, which are public.
//
// Install and register it with Claude Code:
//
//	go install github.com/INTODAN/island-battles-go/clients/island-battles-go/cmd/island-battles-mcp@latest
//	claude mcp add island-battles -- island-battles-mcp
//
// Flags:
//
//	--demo <path>        load a demo file at start
//	--lobby-code <code>  load a lobby's game at start
//	--game-url <url>     the game, https://island-battles.elidan.org by default ($ISLAND_BATTLES_GAME_URL)
//	--lobby-url <url>    the lobby, https://lobby.elidan.org by default ($ISLAND_BATTLES_LOBBY_URL)
//	--name <name>        the watcher name the lobby's host sees, "AI agent" by default
//
// It logs to stderr, because stdout carries the MCP stream.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// The production game and lobby, which players reach by default.
const (
	defaultGameURL  = "https://island-battles.elidan.org"
	defaultLobbyURL = "https://lobby.elidan.org"
)

// options configure the server.
type options struct {
	demoPath    string
	lobbyCode   string
	gameURL     string
	lobbyURL    string
	displayName string
}

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	settings, err := parseOptions(os.Args[1:], os.Getenv, os.Stderr)
	if errors.Is(err, flag.ErrHelp) {
		return
	}
	if err != nil {
		logger.Error("Invalid arguments", "error", err)
		os.Exit(2)
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if err := run(ctx, settings, &mcp.StdioTransport{}, &http.Client{Timeout: 2 * time.Minute}, logger); err != nil {
		logger.Error("The Island Battles MCP server stopped", "error", err)
		os.Exit(1)
	}
}

func parseOptions(arguments []string, getenv func(string) string, output io.Writer) (options, error) {
	flags := flag.NewFlagSet("island-battles-mcp", flag.ContinueOnError)
	flags.SetOutput(output)
	settings := options{}
	flags.StringVar(&settings.demoPath, "demo", "", "a demo file to load at start")
	flags.StringVar(&settings.lobbyCode, "lobby-code", "", "a lobby's join code whose game to load at start")
	flags.StringVar(&settings.gameURL, "game-url", or(getenv("ISLAND_BATTLES_GAME_URL"), defaultGameURL), "the game's URL")
	flags.StringVar(&settings.lobbyURL, "lobby-url", or(getenv("ISLAND_BATTLES_LOBBY_URL"), defaultLobbyURL), "the lobby's URL")
	flags.StringVar(&settings.displayName, "name", "AI agent", "the watcher name that the lobby's host sees, at most 40 characters")
	if err := flags.Parse(arguments); err != nil {
		return options{}, err
	}
	if flags.NArg() > 0 {
		return options{}, fmt.Errorf("unexpected arguments %q", flags.Args())
	}
	if settings.displayName == "" || len([]rune(settings.displayName)) > 40 {
		return options{}, fmt.Errorf("--name must have 1 to 40 characters")
	}
	return settings, nil
}

func or(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

// run loads what the options name and serves the tools over transport until
// the client disconnects or ctx ends.
func run(ctx context.Context, settings options, transport mcp.Transport, client *http.Client, logger *slog.Logger) error {
	games := newStore(newLobbyClient(settings.lobbyURL, settings.gameURL, settings.displayName, client), logger)
	if settings.demoPath != "" {
		if _, err := games.loadDemo(settings.demoPath); err != nil {
			return err
		}
	}
	if settings.lobbyCode != "" {
		// The lobby may be unreachable at start; the agent can load it later.
		if _, err := games.loadLobby(ctx, settings.lobbyCode); err != nil {
			logger.Warn("Couldn't load the lobby's game at start", "code", settings.lobbyCode, "error", err)
		}
	}
	logger.Info("Serving the Island Battles MCP server over stdio", "game_url", settings.gameURL, "lobby_url", settings.lobbyURL)
	if err := newServer(games).Run(ctx, transport); err != nil && !errors.Is(err, context.Canceled) {
		return fmt.Errorf("serve MCP: %w", err)
	}
	return nil
}

func newServer(games *store) *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{Name: "island-battles", Title: "Island Battles", Version: "1.0.0"}, &mcp.ServerOptions{Instructions: instructions})
	addTools(server, games)
	return server
}
