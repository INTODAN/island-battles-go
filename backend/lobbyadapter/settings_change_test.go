package lobbyadapter_test

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"islandBattles/backend/dal"
	"islandBattles/backend/lobbyadapter"
	"islandBattles/backend/models"
	"islandBattles/lobby/domain"

	"github.com/google/uuid"
)

// A three-team map: one castle per team and a neutral one in the middle.
const threeTeamMapFile = `{"map_name":"Delta","player_amount":3,"plane_size_x":10,"plane_size_y":6,"neutral_starting_soldier_amount":5,` +
	`"castles":[{"team_color":1,"x":-4,"y":-2,"starting_soldier_amount":30},{"team_color":2,"x":4,"y":-2,"starting_soldier_amount":30},{"team_color":3,"x":0,"y":2,"starting_soldier_amount":30},{"team_color":0,"x":0,"y":0}]}`

func teamBodies(names ...string) []map[string]any {
	teams := make([]map[string]any, len(names))
	for index, name := range names {
		teams[index] = map[string]any{"name": name}
	}
	return teams
}

func TestTheHostChangesToAMapWithMoreTeamsAndStartsOnIt(t *testing.T) {
	stack := newLobbyStack(t)
	strait := storeTemplate(t, stack.database, twoTeamMapFile)
	delta := storeTemplate(t, stack.database, threeTeamMapFile)
	alice, bob := stack.guest("Alice"), stack.guest("Bob")
	var room domain.Snapshot
	stack.expect(stack.api.Post("/lobby/v1/lobbies", alice.header, map[string]any{
		"request_id": uuid.NewString(), "game_key": lobbyadapter.GameKey, "name": "Maps", "teams": teamBodies("Blue", "Red"),
		"client_instance_id": alice.client, "game_settings": map[string]any{"map_template_id": strait},
	}), http.StatusCreated, &room)
	stack.expect(stack.api.Post("/lobby/v1/memberships", bob.header, map[string]any{"game_key": lobbyadapter.GameKey, "join_code": room.Lobby.JoinCode, "client_instance_id": bob.client}), http.StatusOK, nil)
	for index, member := range []guest{alice, bob} {
		stack.expect(stack.api.Put(stack.lobbyPath(room)+"/team", member.header, map[string]any{"team_id": room.Teams[index].ID}), http.StatusOK, nil)
		stack.expect(stack.choose(room, member, `{"kind":"default","level":1}`), http.StatusOK, nil)
		stack.expect(stack.ready(room, member), http.StatusOK, nil)
	}
	path := stack.lobbyPath(room) + "/settings"

	// Only the host changes the map, and the map must fit the teams sent.
	var refusal problem
	stack.expect(stack.api.Put(path, bob.header, map[string]any{"teams": teamBodies("Blue", "Red", "Green"), "game_settings": map[string]any{"map_template_id": delta}}), http.StatusForbidden, &refusal)
	if refusal.Code != "not_host" {
		t.Fatalf("a guest's change = %+v", refusal)
	}
	for _, mismatch := range []map[string]any{
		{"teams": teamBodies("Blue", "Red", "Green"), "game_settings": map[string]any{"map_template_id": strait}},
		{"teams": teamBodies("Blue", "Red"), "game_settings": map[string]any{"map_template_id": delta}},
		{"teams": teamBodies("Blue", "Red", "Green"), "game_settings": map[string]any{"map_template_id": delta, "turn_timeout_seconds": 4}},
	} {
		stack.expect(stack.api.Put(path, alice.header, mismatch), http.StatusUnprocessableEntity, &refusal)
		if refusal.Code != "invalid_game_settings" || !strings.HasPrefix(refusal.Detail, "game_settings.") {
			t.Fatalf("settings %v = %+v", mismatch, refusal)
		}
	}

	var changed domain.Snapshot
	stack.expect(stack.api.Put(path, alice.header, map[string]any{"teams": teamBodies("Blue", "Red", "Green"), "game_settings": map[string]any{"map_template_id": delta, "turn_timeout_seconds": 120}}), http.StatusOK, &changed)
	if changed.Lobby.Capacity != 3 || len(changed.Teams) != 3 || changed.Teams[0].ID != room.Teams[0].ID || string(changed.Lobby.GameSettings) != fmt.Sprintf(`{"map_template_id":%d,"turn_timeout_seconds":120}`, delta) {
		t.Fatalf("changed = %+v", changed)
	}
	for _, member := range changed.Members {
		if member.Ready || member.TeamID == nil {
			t.Fatalf("member after the change = %+v", member)
		}
	}
	// Bots are in the members' member settings, so they survive the change.
	bots := stack.bots(changed)
	for _, member := range changed.Members {
		if !sameBot(bots[member.PlayerID], botChoice(models.DefaultBot, 1)) {
			t.Fatalf("bots after the change = %+v", bots)
		}
	}

	// A bot takes the new team, the people get ready again, and the game
	// starts on the new map with the new timeout.
	stack.expect(stack.api.Post(stack.lobbyPath(room)+"/seats", alice.header, map[string]any{"team_id": changed.Teams[2].ID, "seat_settings": map[string]any{"bot_level": 1}}), http.StatusOK, nil)
	for _, member := range []guest{alice, bob} {
		stack.expect(stack.ready(room, member), http.StatusOK, nil)
	}
	var started domain.Snapshot
	stack.expect(stack.api.Post(stack.lobbyPath(room)+"/start", alice.header, map[string]any{}), http.StatusOK, &started)
	if len(started.StartRoster) != 3 || started.StartRoster[2].TeamID != changed.Teams[2].ID {
		t.Fatalf("started = %+v", started)
	}
	state := stack.state(room, alice)
	game, err := dal.GetGameByID(stack.database, state.GameID)
	if err != nil {
		t.Fatal(err)
	}
	if started := game.Generations[0].Map; started.MapName != "Delta" || len(started.Castles) != 4 {
		t.Fatalf("started map = %+v", started.MapFields)
	}
	if game.TurnTimeoutSeconds == nil || *game.TurnTimeoutSeconds != 120 {
		t.Fatalf("turn timeout = %v", game.TurnTimeoutSeconds)
	}
	// Once started, the settings are fixed.
	stack.expect(stack.api.Put(path, alice.header, map[string]any{"teams": teamBodies("Blue", "Red"), "game_settings": map[string]any{"map_template_id": strait}}), http.StatusConflict, &refusal)
	if refusal.Code != "phase_conflict" {
		t.Fatalf("a change after the start = %+v", refusal)
	}
}
