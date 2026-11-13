package lobbyadapter_test

import (
	"net/http"
	"testing"

	"github.com/INTODAN/mpl/domain"
	"islandBattles/backend/dal"
	"islandBattles/backend/lobbyadapter"
	"islandBattles/backend/models"

	"github.com/google/uuid"
)

// soloRoom is a lobby of two one-seat teams that host created, on the first
// team, with nobody else.
func (stack *lobbyStack) soloRoom(gameKey string, host guest) domain.Snapshot {
	stack.t.Helper()
	var snapshot domain.Snapshot
	stack.expect(stack.api.Post("/lobby/v1/lobbies", host.header, map[string]any{
		"request_id": uuid.NewString(), "game_key": gameKey, "name": "Solo",
		"teams": []map[string]any{{"name": "Red"}, {"name": "Blue"}}, "client_instance_id": host.client,
	}), http.StatusCreated, &snapshot)
	stack.expect(stack.api.Put(stack.lobbyPath(snapshot)+"/team", host.header, map[string]any{"team_id": snapshot.Teams[0].ID}), http.StatusOK, nil)
	return snapshot
}

type problem struct {
	Code   string `json:"code"`
	Detail string `json:"detail"`
}

func TestTheHostPlaysAloneAgainstABotSeat(t *testing.T) {
	stack := newLobbyStack(t)
	alice, bob := stack.guest("Alice"), stack.guest("Bob")
	room := stack.soloRoom(lobbyadapter.GameKey, alice)
	path := stack.lobbyPath(room) + "/seats"

	// The game checks the seat's settings against its default bots.
	for _, invalid := range []any{map[string]any{}, map[string]any{"bot_level": 4}, map[string]any{"bot_level": 0}, map[string]any{"bot_level": 2, "name": "Rex"}, map[string]any{"bot_level": "2"}} {
		var refusal problem
		stack.expect(stack.api.Post(path, alice.header, map[string]any{"team_id": room.Teams[1].ID, "seat_settings": invalid}), http.StatusUnprocessableEntity, &refusal)
		if refusal.Code != "invalid_seat_settings" || refusal.Detail != `seat_settings must be {"bot_level": N} with a level that GET /bots/defaults lists: 1, 2, 3` {
			t.Fatalf("seat settings %v = %+v", invalid, refusal)
		}
	}
	var seated domain.Snapshot
	stack.expect(stack.api.Post(path, alice.header, map[string]any{"team_id": room.Teams[1].ID, "seat_settings": map[string]any{"bot_level": 2}}), http.StatusOK, &seated)
	seat := seated.Members[1]
	if !seat.IsSeat() || seat.DisplayName != "Bot (level 2)" || string(seat.SeatSettings) != `{"bot_level":2}` || !seat.Ready || *seat.TeamID != room.Teams[1].ID {
		t.Fatalf("seat = %+v", seat)
	}
	// The lobby is full, so nobody else can join or be seated.
	stack.expect(stack.api.Post("/lobby/v1/memberships", bob.header, map[string]any{"game_key": lobbyadapter.GameKey, "join_code": room.Lobby.JoinCode, "client_instance_id": bob.client}), http.StatusConflict, nil)

	// The seat's bot is in its seat settings, not in member settings, and
	// the host still chooses their own.
	if bots := stack.bots(seated); len(bots) != 0 {
		t.Fatalf("bots = %+v", bots)
	}
	stack.expect(stack.ready(room, alice), http.StatusConflict, nil)
	stack.expect(stack.choose(room, alice, `{"kind":"localhost"}`), http.StatusOK, nil)
	stack.expect(stack.ready(room, alice), http.StatusOK, nil)

	var started domain.Snapshot
	stack.expect(stack.api.Post(stack.lobbyPath(room)+"/start", alice.header, map[string]any{}), http.StatusOK, &started)
	if started.Lobby.Phase != domain.PhaseStarted || len(started.StartRoster) != 2 || started.StartRoster[1].PlayerID != seat.PlayerID {
		t.Fatalf("started = %+v", started)
	}
	// The game copied the seat to its roster as a level 2 Go bot, which has
	// played the first turn; Alice's own client plays hers.
	state := stack.state(room, alice)
	if !sameBot(&state.Bot, botChoice(models.LocalhostBot, 0)) || state.Submitted {
		t.Fatalf("Alice's state = %+v", state)
	}
	players, err := dal.LoadFrozenPlayers(stack.database, state.GameID)
	if err != nil || len(players) != 2 {
		t.Fatalf("frozen players = %+v, %v", players, err)
	}
	for _, player := range players {
		if player.PlayerID == seat.PlayerID && (player.BotKind != models.DefaultBot || player.BotLevel == nil || *player.BotLevel != 2 || player.DisplayName != "Bot (level 2)") {
			t.Fatalf("the seat's frozen player = %+v", player)
		}
		if player.PlayerID == alice.playerID && (player.BotKind != models.LocalhostBot || player.BotLevel != nil || player.BotURL != nil || player.BotPackageID != nil) {
			t.Fatalf("Alice's frozen player = %+v", player)
		}
	}
	inputs, err := dal.LoadGenerationInputs(stack.database, state.GenerationID)
	if err != nil {
		t.Fatal(err)
	}
	if !completedBy(inputs, seat.PlayerID) {
		t.Fatalf("the bot seat did not play the first turn: %+v", inputs)
	}
	// Once started, the seats are fixed.
	stack.expect(stack.api.Delete(path+"/"+seat.ID, alice.header), http.StatusConflict, nil)
}

func completedBy(inputs []models.GenerationInput, playerID string) bool {
	for _, input := range inputs {
		if input.PlayerID == playerID && input.Completed {
			return true
		}
	}
	return false
}

func TestOnlyTheHostSeatsABotWhileTheLobbyWaits(t *testing.T) {
	stack := newLobbyStack(t)
	alice, bob := stack.guest("Alice"), stack.guest("Bob")
	room := stack.soloRoom(lobbyadapter.GameKey, alice)
	stack.expect(stack.api.Post("/lobby/v1/memberships", bob.header, map[string]any{"game_key": lobbyadapter.GameKey, "join_code": room.Lobby.JoinCode, "client_instance_id": bob.client}), http.StatusOK, nil)
	var refusal problem
	stack.expect(stack.api.Post(stack.lobbyPath(room)+"/seats", bob.header, map[string]any{"team_id": room.Teams[1].ID, "seat_settings": map[string]any{"bot_level": 1}}), http.StatusForbidden, &refusal)
	if refusal.Code != "not_host" {
		t.Fatalf("Bob seating a bot = %+v", refusal)
	}
	// Bob leaves, and Alice seats a bot in his place, then removes it.
	stack.expect(stack.api.Delete(stack.lobbyPath(room)+"/membership", bob.header), http.StatusNoContent, nil)
	var seated, removed domain.Snapshot
	stack.expect(stack.api.Post(stack.lobbyPath(room)+"/seats", alice.header, map[string]any{"team_id": room.Teams[1].ID, "seat_settings": map[string]any{"bot_level": 1}}), http.StatusOK, &seated)
	stack.expect(stack.api.Delete(stack.lobbyPath(room)+"/seats/"+seated.Members[1].ID, alice.header), http.StatusOK, &removed)
	if len(removed.Members) != 1 {
		t.Fatalf("members after removing the seat = %+v", removed.Members)
	}
}
