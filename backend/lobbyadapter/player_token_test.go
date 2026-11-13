package lobbyadapter_test

import (
	"net/http"
	"strconv"
	"strings"
	"testing"

	"islandBattles/backend/lobbyadapter"
	"islandBattles/backend/models"
)

// The game's routes take the lobby's player tokens and refuse the guest
// credential, and they never read the lobby's sessions.
func TestGameRoutesTakePlayerTokensAndRefuseTheGuestCredential(t *testing.T) {
	stack := newLobbyStack(t)
	alice, bob := stack.guest("Alice"), stack.guest("Bob")
	room := stack.room(lobbyadapter.GameKey, alice, bob)
	for _, member := range []guest{alice, bob} {
		stack.expect(stack.choose(room, member, `{"kind":"localhost"}`), http.StatusOK, nil)
		stack.expect(stack.ready(room, member), http.StatusOK, nil)
	}
	stack.expect(stack.api.Post(stack.lobbyPath(room)+"/start", alice.header, map[string]any{}), http.StatusOK, nil)
	generation := "/game/from-lobby/" + room.Lobby.ID + "/generation"

	token := stack.playerToken(alice, lobbyadapter.GameKey)
	var state models.GenerationState
	stack.expect(stack.api.Get(generation, token), http.StatusOK, &state)
	if state.PlayerID != alice.playerID {
		t.Fatalf("player = %s, want Alice %s", state.PlayerID, alice.playerID)
	}

	altered := token[:len(token)-2] + "AA"
	if strings.HasSuffix(token, "AA") {
		altered = token[:len(token)-2] + "BB"
	}
	for name, header := range map[string]string{
		"the guest credential":         alice.header,
		"a token for another game":     stack.playerToken(alice, mockGameKey),
		"an altered token":             altered,
		"no credential":                "Accept: application/json",
		"a token without its scheme":   "Authorization: " + strings.TrimPrefix(token, "Authorization: Bearer "),
		"a token of a lobby's session": "Authorization: Bearer " + strings.Repeat("A", 43),
	} {
		var problem struct {
			Status int    `json:"status"`
			Detail string `json:"detail"`
		}
		stack.expect(stack.api.Get(generation, header), http.StatusUnauthorized, &problem)
		if problem.Detail != "authentication required" {
			t.Errorf("%s: problem = %+v", name, problem)
		}
	}

	// Without the lobby's sessions, the token still serves every game route
	// that reads, and the commands.
	if err := stack.lobbyDatabase.Exec("DELETE FROM lobby_sessions").Error; err != nil {
		t.Fatal(err)
	}
	stack.expect(stack.api.Get(generation, token), http.StatusOK, &state)
	for _, path := range []string{"/logs?generation_id=" + strconv.FormatUint(uint64(state.GenerationID), 10), "/demo", "/watch"} {
		stack.expect(stack.api.Get("/game/from-lobby/"+room.Lobby.ID+path, token), http.StatusOK, nil)
	}
	stack.expect(stack.api.Post("/game/from-lobby/"+room.Lobby.ID+"/deadline", token, map[string]any{}), http.StatusOK, nil)
	stack.expect(stack.api.Post("/generation/commands", token, map[string]any{
		"game_id": state.GameID, "generation_id": state.GenerationID, "request_id": "8c1c7c55-3c8f-4c55-9a3e-7c6b1b2d3e4f", "complete": true, "commands": []any{},
	}), http.StatusOK, nil)
	// The lobby itself still needs the session.
	stack.expect(stack.api.Post("/lobby/v1/player-tokens", alice.header, map[string]string{"game_key": lobbyadapter.GameKey}), http.StatusUnauthorized, nil)
}
