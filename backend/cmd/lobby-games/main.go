// Command lobby-games prints the LOBBY_GAMES of a lobby that runs on its own
// (lobby/cmd/lobby) with Island Battles in it: the LOBBY_GAMES of its
// environment, plus Island Battles' entry (backend/lobbyadapter's
// lobby_game.json) with its hooks at the backend's origin, unless that
// LOBBY_GAMES lists island-battles itself. scripts/dev.sh and the live test
// scripts configure the local lobby with it:
//
//	LOBBY_GAMES="$(go run ./backend/cmd/lobby-games http://localhost:8888)"
//
// The backend's origin is where the lobby reaches it, such as
// http://localhost:8888; the hooks are at its /game/lobby-hooks. It is also
// where browsers reach the game: the lobby page, on the lobby's own origin,
// loads Island Battles' modules and documents from it, so the entry's
// catalog has absolute URLs there, with that origin as its origin. The
// lobby accepts such an origin when it is https, or http on localhost.
package main

import (
	"fmt"
	"io"
	"os"
	"strings"

	"islandBattles/backend/dependencies"
	"islandBattles/lobby/gamehooks"
)

func main() {
	if err := run(os.Args[1:], os.LookupEnv, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "lobby-games:", err)
		os.Exit(2)
	}
}

func run(args []string, lookup func(string) (string, bool), output io.Writer) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: lobby-games <the backend's origin, such as http://localhost:8888>")
	}
	origin := strings.TrimRight(args[0], "/")
	parsed, err := gamehooks.ParseServiceURL(origin)
	if err != nil {
		return err
	}
	if parsed.Path != "" {
		return fmt.Errorf("%q must be an origin, without a path", origin)
	}
	existing, _ := lookup(dependencies.LobbyGamesEnv)
	games, err := dependencies.Config{PublicURL: origin, LobbyGames: existing}.LobbyGamesConfig(origin)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(output, games)
	return err
}
