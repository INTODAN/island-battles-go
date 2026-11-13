package lobbyadapter_test

import (
	"net/http"
	"testing"

	"islandBattles/backend/lobbyadapter"
	"islandBattles/lobby/domain"
	"islandBattles/lobby/gamehooks"
)

// The game authorizes a lobby's watchers through the lobby's game API: a
// watcher with the join code reads the game, a watcher the host kicked gets
// 403, and anyone else 404.
func TestWatchersAreAuthorizedThroughTheGameAPI(t *testing.T) {
	stack := newLobbyStack(t)
	alice, bob, watcher, stranger := stack.guest("Alice"), stack.guest("Bob"), stack.guest("Watcher"), stack.guest("Stranger")
	room := stack.room(lobbyadapter.GameKey, alice, bob)
	for _, member := range []guest{alice, bob} {
		stack.expect(stack.choose(room, member, `{"kind":"localhost"}`), http.StatusOK, nil)
		stack.expect(stack.ready(room, member), http.StatusOK, nil)
	}
	stack.expect(stack.api.Post("/lobby/v1/watches", watcher.header, map[string]any{"game_key": lobbyadapter.GameKey, "join_code": room.Lobby.JoinCode}), http.StatusOK, nil)
	var started domain.Snapshot
	stack.expect(stack.api.Post(stack.lobbyPath(room)+"/start", alice.header, map[string]any{}), http.StatusOK, &started)
	if started.Lobby.Phase != domain.PhaseStarted {
		t.Fatalf("phase = %s", started.Lobby.Phase)
	}
	watch := func(member guest) int {
		t.Helper()
		return stack.api.Get("/game/from-lobby/"+room.Lobby.ID+"/watch", stack.playerToken(member, lobbyadapter.GameKey)).Code
	}
	calls := stack.hookCalls[gamehooks.HookStart]
	if status := watch(watcher); status != http.StatusOK {
		t.Fatalf("the watcher's read = %d, want 200", status)
	}
	if status := watch(stranger); status != http.StatusNotFound {
		t.Fatalf("a stranger's read = %d, want 404", status)
	}
	stack.expect(stack.api.Delete(stack.lobbyPath(room)+"/watchers/"+watcher.playerID, alice.header), http.StatusNoContent, nil)
	if status := watch(watcher); status != http.StatusForbidden {
		t.Fatalf("the kicked watcher's read = %d, want 403", status)
	}
	if stack.hookCalls[gamehooks.HookStart] != calls {
		t.Fatal("reading the game called the start hook")
	}
}
