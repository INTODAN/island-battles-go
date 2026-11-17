package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
)

// liveRefresh is how old a running game read by lobby code may be before a
// tool reads it again.
const liveRefresh = 15 * time.Second

// store holds the loaded games. The tools answer about the game their game
// argument names, or the one loaded last.
type store struct {
	lobby  *lobbyClient
	logger *slog.Logger

	mutex   sync.Mutex
	games   map[string]*Game
	current string
}

func newStore(lobby *lobbyClient, logger *slog.Logger) *store {
	return &store{lobby: lobby, logger: logger, games: map[string]*Game{}}
}

// loadDemo reads a demo file and makes it the current game.
func (store *store) loadDemo(path string) (*Game, error) {
	expanded, err := expandHome(strings.TrimSpace(path))
	if err != nil {
		return nil, err
	}
	demo, err := readDemoFile(expanded)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", expanded, err)
	}
	game := newGame(filepath.Base(expanded), "demo file "+expanded, demo)
	store.put(game)
	return game, nil
}

// loadLobby reads a lobby's game by its join code and makes it the current
// game. Loading it again reads it again.
func (store *store) loadLobby(ctx context.Context, code string) (*Game, error) {
	game, err := store.readLobby(ctx, code)
	if err != nil {
		return nil, err
	}
	store.put(game)
	return game, nil
}

// readLobby reads a lobby's game by its join code.
func (store *store) readLobby(ctx context.Context, code string) (*Game, error) {
	code = normalizeCode(code)
	if code == "" {
		return nil, fmt.Errorf("give the lobby's code, as the lobby page shows it")
	}
	demo, err := store.lobby.readGame(ctx, code)
	if err != nil {
		return nil, err
	}
	game := newGame(code, "lobby code "+code+" on "+store.lobby.gameURL, demo)
	game.LobbyCode = code
	return game, nil
}

func (store *store) put(game *Game) {
	store.mutex.Lock()
	defer store.mutex.Unlock()
	store.games[game.Key] = game
	store.current = game.Key
}

// get returns the game named key, or the current one when key is empty. A
// running game read by lobby code is read again once it is liveRefresh old,
// so a question about the current turn sees it.
func (store *store) get(ctx context.Context, key string) (*Game, error) {
	store.mutex.Lock()
	if key == "" {
		key = store.current
	}
	game, found := store.games[key]
	if !found {
		game, found = store.games[normalizeCode(key)]
	}
	keys := store.keysLocked()
	store.mutex.Unlock()
	if !found {
		if len(keys) == 0 {
			return nil, fmt.Errorf("no game is loaded: call load_demo with a demo file's path, or load_lobby with a lobby's code")
		}
		return nil, fmt.Errorf("no game %q is loaded; the loaded games are %s", key, strings.Join(keys, ", "))
	}
	if game.LobbyCode != "" && game.Demo.Outcome == nil && time.Since(game.LoadedAt) > liveRefresh {
		refreshed, err := store.readLobby(ctx, game.LobbyCode)
		if err != nil {
			store.logger.Warn("Couldn't read the game again; answering from the copy read before", "code", game.LobbyCode, "error", err)
			return game, nil
		}
		// A refresh doesn't change which game is current.
		store.mutex.Lock()
		store.games[refreshed.Key] = refreshed
		store.mutex.Unlock()
		return refreshed, nil
	}
	return game, nil
}

// keys lists the loaded games' keys.
func (store *store) keys() []string {
	store.mutex.Lock()
	defer store.mutex.Unlock()
	return store.keysLocked()
}

func (store *store) keysLocked() []string {
	keys := make([]string, 0, len(store.games))
	for key := range store.games {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys
}

// normalizeCode trims a lobby code and writes it in capitals, as the lobby
// reads codes.
func normalizeCode(code string) string {
	return strings.ToUpper(strings.TrimSpace(code))
}

// expandHome replaces a leading ~ with the user's home directory, which a
// prompt's path may use.
func expandHome(path string) (string, error) {
	if path == "~" || strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, strings.TrimPrefix(path, "~")), nil
	}
	return path, nil
}
