package main

import (
	"bytes"
	"encoding/json"
	"testing"
)

func lobbyGames(t *testing.T, existing string, args ...string) ([]map[string]any, error) {
	t.Helper()
	var output bytes.Buffer
	err := run(args, func(name string) (string, bool) {
		if name == "LOBBY_GAMES" && existing != "" {
			return existing, true
		}
		return "", false
	}, &output)
	if err != nil {
		return nil, err
	}
	var games []map[string]any
	if err := json.Unmarshal(output.Bytes(), &games); err != nil {
		t.Fatalf("output %q: %v", output.String(), err)
	}
	return games, nil
}

func TestIslandBattlesIsAddedWithItsHooksAtTheOrigin(t *testing.T) {
	games, err := lobbyGames(t, `[{"key":"mock-game","name":"Mock game","hooks_url":"http://127.0.0.1:9/hooks","secret_env":"LOBBY_GAME_SECRET_MOCK_GAME"}]`, "http://localhost:8888/")
	if err != nil {
		t.Fatal(err)
	}
	if len(games) != 2 || games[0]["key"] != "mock-game" || games[1]["key"] != "island-battles" ||
		games[1]["hooks_url"] != "http://localhost:8888/game/lobby-hooks" || games[1]["frontend"] == nil {
		t.Fatalf("games = %v", games)
	}
	// The lobby page is on the lobby's origin, so the game's catalog entry
	// points at the backend's.
	catalog := games[1]["frontend"].(map[string]any)
	if catalog["origin"] != "http://localhost:8888" || catalog["settings_module"] != "http://localhost:8888/game/demo/lobby-settings.mjs" ||
		catalog["result_url"] != "http://localhost:8888/game/from-lobby/{lobby_id}/replay?lobby_round={round}" {
		t.Fatalf("catalog = %v", catalog)
	}
}

func TestALobbyGamesThatListsIslandBattlesIsKept(t *testing.T) {
	games, err := lobbyGames(t, `[{"key":"island-battles","name":"Island Battles","hooks_url":"https://game.example.org/game/lobby-hooks","secret_env":"S"}]`, "http://localhost:8888")
	if err != nil {
		t.Fatal(err)
	}
	if len(games) != 1 || games[0]["hooks_url"] != "https://game.example.org/game/lobby-hooks" {
		t.Fatalf("games = %v", games)
	}
}

func TestTheOriginMustBeAnOrigin(t *testing.T) {
	for _, args := range [][]string{{}, {"http://example.org"}, {"http://localhost:8888/game"}, {"a", "b"}} {
		if _, err := lobbyGames(t, "", args...); err == nil {
			t.Errorf("run(%q) succeeded", args)
		}
	}
}
