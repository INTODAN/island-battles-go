package lobbyadapter_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/INTODAN/mpl/domain"
	"github.com/INTODAN/mpl/gamehooks"
	"github.com/google/uuid"
	"islandBattles/backend/dal"
	"islandBattles/backend/lobbyadapter"
	"islandBattles/backend/models"
	"islandBattles/backend/services"
)

// The host seats several localhost bots, each on its own port of their
// machine, next to their own localhost bot, and relays them all: the game
// lists the seats in the host's state, with their ports.
func TestTheHostSeatsLocalhostBotsEachOnItsOwnPort(t *testing.T) {
	stack := newLobbyStack(t)
	alice := stack.guest("Alice")
	room := stack.teamsRoom(alice, "Red", "Blue", "Green", "Gold")
	stack.expect(stack.choose(room, alice, `{"kind":"localhost"}`), http.StatusOK, nil)

	// A port is a number, or the digits that the lobby page's text field
	// sends; the seat is named after it.
	var seated domain.Snapshot
	stack.expect(stack.addSeat(room, alice, 1, localhostSeat("41415", alice)), http.StatusOK, &seated)
	if seat := seatOn(t, seated, 1); seat.DisplayName != "Localhost bot (port 41415)" {
		t.Fatalf("seat = %+v", seat)
	}
	stack.expect(stack.addSeat(room, alice, 2, localhostSeat(41416, alice)), http.StatusOK, &seated)
	if seat := seatOn(t, seated, 2); seat.DisplayName != "Localhost bot (port 41416)" {
		t.Fatalf("seat = %+v", seat)
	}

	// Each of the host's localhost bots needs a port of its own, and a port
	// is not a privileged one. The seat names the host who adds it.
	shape := `{"bot_localhost_port": N, "bot_relay_player_id": "<your player ID>"} with a port from 1024 to 65535`
	for _, refused := range []struct {
		settings map[string]any
		detail   string
	}{
		{localhostSeat("41414", alice), "port 41414 is already taken by your own localhost bot"},
		{localhostSeat(80, alice), "a localhost bot's port must be from 1024 to 65535"},
		{localhostSeat(70000, alice), "a localhost bot's port must be from 1024 to 65535"},
		{localhostSeat("41 417", alice), shape},
		{map[string]any{"bot_localhost_port": 41417, "bot_relay_player_id": alice.playerID, "bot_level": 1}, shape},
		{map[string]any{"bot_localhost_port": 41417}, shape},
		{map[string]any{"bot_level": 1, "bot_relay_player_id": alice.playerID}, shape},
		{map[string]any{"bot_localhost_port": 41417, "bot_relay_player_id": "alice"}, shape},
		{map[string]any{"bot_localhost_port": 41417, "bot_relay_player_id": uuid.NewString()}, "bot_relay_player_id must be your own player ID"},
	} {
		var refusal problem
		stack.expect(stack.addSeat(room, alice, 3, refused.settings), http.StatusUnprocessableEntity, &refusal)
		if refusal.Code != "invalid_seat_settings" || !strings.Contains(refusal.Detail, refused.detail) {
			t.Fatalf("seat %v = %+v", refused.settings, refusal)
		}
	}
	// The first port that her bots leave free is suggested.
	var refusal problem
	stack.expect(stack.addSeat(room, alice, 3, localhostSeat(41414, alice)), http.StatusUnprocessableEntity, &refusal)
	if !strings.Contains(refusal.Detail, "such as 41417") {
		t.Fatalf("refusal = %+v", refusal)
	}
	stack.expect(stack.addSeat(room, alice, 3, map[string]any{"bot_level": 1}), http.StatusOK, nil)

	// The host's own port is checked again at the start, since the member
	// settings' check doesn't know whose they are.
	stack.expect(stack.choose(room, alice, `{"kind":"localhost","port":41416}`), http.StatusOK, nil)
	stack.expect(stack.ready(room, alice), http.StatusOK, nil)
	var taken gameRequirement
	stack.expect(stack.api.Post(stack.lobbyPath(room)+"/start", alice.header, map[string]any{}), http.StatusConflict, &taken)
	if taken.Code != "game_requirement" || taken.Type != services.LocalhostPortTakenProblemType || !strings.Contains(taken.Detail, "port 41416") {
		t.Fatalf("start = %+v", taken)
	}
	stack.expect(stack.choose(room, alice, `{"kind":"localhost","port":41420}`), http.StatusOK, nil)
	stack.expect(stack.ready(room, alice), http.StatusOK, nil)
	var started domain.Snapshot
	stack.expect(stack.api.Post(stack.lobbyPath(room)+"/start", alice.header, map[string]any{}), http.StatusOK, &started)

	// Alice relays her own bot on her port and the two localhost seats on
	// theirs; the level 1 seat is the backend's.
	state := stack.state(room, alice)
	if state.Bot.Kind != models.LocalhostBot || state.Bot.Port == nil || *state.Bot.Port != 41420 {
		t.Fatalf("Alice's bot = %+v", state.Bot)
	}
	first, second := seatOn(t, started, 1), seatOn(t, started, 2)
	want := []models.RelayedBotView{
		{PlayerID: first.PlayerID, TeamColor: 2, DisplayName: "Localhost bot (port 41415)", Port: 41415},
		{PlayerID: second.PlayerID, TeamColor: 3, DisplayName: "Localhost bot (port 41416)", Port: 41416},
	}
	if len(state.RelayedBots) != len(want) || state.RelayedBots[0] != want[0] || state.RelayedBots[1] != want[1] {
		t.Fatalf("relayed bots = %+v, want %+v", state.RelayedBots, want)
	}
	players, err := dal.LoadFrozenPlayers(stack.database, state.GameID)
	if err != nil || len(players) != 4 {
		t.Fatalf("frozen players = %+v, %v", players, err)
	}
	for _, player := range players {
		switch player.PlayerID {
		case first.PlayerID, second.PlayerID:
			if player.BotKind != models.LocalhostBot || player.BotRelayPlayerID == nil || *player.BotRelayPlayerID != state.PlayerID {
				t.Fatalf("seat %+v", player)
			}
		case state.PlayerID:
			if player.BotRelayPlayerID != nil || player.BotPort == nil || *player.BotPort != 41420 {
				t.Fatalf("Alice %+v", player)
			}
		default:
			if player.BotKind != models.DefaultBot || player.BotPort != nil {
				t.Fatalf("level 1 seat %+v", player)
			}
		}
	}
}

// A member's own localhost bot takes a port, and only it does.
func TestALocalhostBotsPortIsChecked(t *testing.T) {
	stack := newLobbyStack(t)
	alice, bob := stack.guest("Alice"), stack.guest("Bob")
	room := stack.room(lobbyadapter.GameKey, alice, bob)
	stack.expect(stack.choose(room, alice, `{"kind":"localhost","port":50000}`), http.StatusOK, nil)
	for _, invalid := range []string{`{"kind":"localhost","port":1023}`, `{"kind":"localhost","port":65536}`, `{"kind":"default","level":1,"port":41415}`, `{"kind":"localhost","port":"41415"}`} {
		stack.expect(stack.choose(room, alice, invalid), http.StatusUnprocessableEntity, nil)
	}
	// Bob's localhost bot is on his own machine, so it may share Alice's port.
	stack.expect(stack.choose(room, bob, `{"kind":"localhost","port":50000}`), http.StatusOK, nil)
	stack.expect(stack.ready(room, alice), http.StatusOK, nil)
	stack.expect(stack.ready(room, bob), http.StatusOK, nil)
	stack.expect(stack.api.Post(stack.lobbyPath(room)+"/start", alice.header, map[string]any{}), http.StatusOK, nil)
	state := stack.state(room, bob)
	if state.Bot.Port == nil || *state.Bot.Port != 50000 || len(state.RelayedBots) != 0 {
		t.Fatalf("Bob's state = %+v", state)
	}
}

// The lobby tells the seat check who adds the seat, so the seat names that
// player and the lobby's own check decides whether they are the host (#302).
// A lobby older than INTODAN/mpl v0.4.0 tells nobody, and then the seat must
// name the current host.
func TestALocalhostSeatNamesThePlayerWhoAddsIt(t *testing.T) {
	stack := newLobbyStack(t)
	alice, bob := stack.guest("Alice"), stack.guest("Bob")
	room := stack.teamsRoom(alice, "Red", "Blue", "Green")
	stack.join(room, bob, 2)

	// Bob names himself, which the seat check accepts, but he isn't the host.
	var refusal problem
	stack.expect(stack.addSeat(room, bob, 1, localhostSeat(41415, bob)), http.StatusForbidden, &refusal)
	if refusal.Code != "not_host" {
		t.Fatalf("Bob's seat = %+v", refusal)
	}
	stack.expect(stack.addSeat(room, alice, 1, localhostSeat(41415, bob)), http.StatusUnprocessableEntity, &refusal)
	if refusal.Code != "invalid_seat_settings" || !strings.Contains(refusal.Detail, "bot_relay_player_id must be your own player ID") {
		t.Fatalf("Alice's seat for Bob = %+v", refusal)
	}

	// An older lobby's validate-seat has no player_id.
	client, err := gamehooks.NewClient(lobbyadapter.GameKey, stack.hooksURL, hookSecret)
	if err != nil {
		t.Fatal(err)
	}
	seat := func(adder guest) json.RawMessage {
		settings, err := json.Marshal(localhostSeat(41415, adder))
		if err != nil {
			t.Fatal(err)
		}
		return settings
	}
	if name, err := client.ValidateSeat(t.Context(), room.Lobby.ID, "", seat(alice)); err != nil || name != "Localhost bot (port 41415)" {
		t.Fatalf("the host's seat without player_id = %q, %v", name, err)
	}
	if _, err := client.ValidateSeat(t.Context(), room.Lobby.ID, "", seat(bob)); err == nil || errors.Is(err, domain.ErrUnavailable) || !strings.Contains(err.Error(), "bot_relay_player_id must be your own player ID") {
		t.Fatalf("Bob's seat without player_id = %v", err)
	}
}

// localhostSeat are the seat settings of a localhost bot seat on port, a
// number or its digits, that adder adds.
func localhostSeat(port any, adder guest) map[string]any {
	return map[string]any{"bot_localhost_port": port, "bot_relay_player_id": adder.playerID}
}

// activeSeats counts a snapshot's active seats.
func activeSeats(snapshot domain.Snapshot) int {
	count := 0
	for _, member := range snapshot.Members {
		if member.IsSeat() && member.Active {
			count++
		}
	}
	return count
}

// A seat request sent again after a lost answer, with its request ID, finds
// its own seat, though the lobby checks the seat's settings before its
// request ID. The same seat added twice, under two request IDs, stops the
// start instead.
func TestALocalhostSeatRequestSentAgainFindsItsSeat(t *testing.T) {
	stack := newLobbyStack(t)
	alice, bob := stack.guest("Alice"), stack.guest("Bob")
	room := stack.teamsRoom(alice, "Red", "Blue", "Green", "Gold")
	stack.join(room, bob, 3)
	request := map[string]any{"request_id": uuid.NewString(), "team_id": room.Teams[1].ID, "seat_settings": localhostSeat("41415", alice)}
	var first, again domain.Snapshot
	stack.expect(stack.api.Post(stack.lobbyPath(room)+"/seats", alice.header, request), http.StatusOK, &first)
	stack.expect(stack.api.Post(stack.lobbyPath(room)+"/seats", alice.header, request), http.StatusOK, &again)
	if activeSeats(first) != 1 || activeSeats(again) != 1 || seatOn(t, again, 1).ID != seatOn(t, first, 1).ID {
		t.Fatalf("seats = %+v, then %+v", first.Members, again.Members)
	}

	stack.expect(stack.addSeat(room, alice, 2, localhostSeat(41415, alice)), http.StatusOK, nil)
	for _, member := range []guest{alice, bob} {
		stack.expect(stack.choose(room, member, `{"kind":"default","level":1}`), http.StatusOK, nil)
		stack.expect(stack.ready(room, member), http.StatusOK, nil)
	}
	var taken gameRequirement
	stack.expect(stack.api.Post(stack.lobbyPath(room)+"/start", alice.header, map[string]any{}), http.StatusConflict, &taken)
	if taken.Type != services.LocalhostPortTakenProblemType || !strings.Contains(taken.Detail, "that Alice relays use port 41415") {
		t.Fatalf("start = %+v", taken)
	}
}

// Only the player who added a localhost bot seat relays it, so a host never
// chooses the local ports that someone else's tab contacts. When the host
// leaves, the next host doesn't relay their seats, and the start is refused
// until those seats are gone, since nobody would play their turns.
func TestOnlyTheAdderRelaysALocalhostSeat(t *testing.T) {
	stack := newLobbyStack(t)
	alice, bob := stack.guest("Alice"), stack.guest("Bob")
	room := stack.teamsRoom(alice, "Red", "Blue", "Green")
	stack.join(room, bob, 2)
	stack.expect(stack.choose(room, bob, `{"kind":"localhost"}`), http.StatusOK, nil)
	// Alice may not seat a bot that Bob would relay.
	var refusal problem
	stack.expect(stack.addSeat(room, alice, 1, localhostSeat(41415, bob)), http.StatusUnprocessableEntity, &refusal)
	if !strings.Contains(refusal.Detail, "bot_relay_player_id must be your own player ID") {
		t.Fatalf("refusal = %+v", refusal)
	}
	stack.expect(stack.addSeat(room, alice, 1, localhostSeat(41415, alice)), http.StatusOK, nil)

	// Alice leaves, and Bob is the host. He can't add a seat in Alice's name;
	// his own seats may take the port of hers, which his tab doesn't relay.
	stack.expect(stack.api.Delete(stack.lobbyPath(room)+"/membership", alice.header), http.StatusNoContent, nil)
	stack.expect(stack.addSeat(room, bob, 0, localhostSeat(41416, alice)), http.StatusUnprocessableEntity, &refusal)
	if !strings.Contains(refusal.Detail, "bot_relay_player_id must be your own player ID") {
		t.Fatalf("refusal = %+v", refusal)
	}
	stack.expect(stack.addSeat(room, bob, 0, localhostSeat(41414, bob)), http.StatusUnprocessableEntity, &refusal)
	if !strings.Contains(refusal.Detail, "port 41414 is already taken by your own localhost bot") {
		t.Fatalf("refusal = %+v", refusal)
	}
	var seated domain.Snapshot
	stack.expect(stack.addSeat(room, bob, 0, localhostSeat(41415, bob)), http.StatusOK, &seated)
	stack.expect(stack.ready(room, bob), http.StatusOK, nil)
	var gone gameRequirement
	stack.expect(stack.api.Post(stack.lobbyPath(room)+"/start", bob.header, map[string]any{}), http.StatusConflict, &gone)
	if gone.Type != services.LocalhostSeatRelayGoneProblemType || gone.Detail != "Remove Localhost bot (port 41415): the player who added it left, and only they can relay it. The host can add it again from their own page." {
		t.Fatalf("start = %+v", gone)
	}

	// Bob replaces Alice's seat with a default bot, and starts.
	stack.expect(stack.api.Delete(stack.lobbyPath(room)+"/seats/"+seatOn(t, seated, 1).ID, bob.header), http.StatusOK, nil)
	stack.expect(stack.addSeat(room, bob, 1, map[string]any{"bot_level": 1}), http.StatusOK, nil)
	var started domain.Snapshot
	stack.expect(stack.api.Post(stack.lobbyPath(room)+"/start", bob.header, map[string]any{}), http.StatusOK, &started)
	bobsSeat := seatOn(t, started, 0)
	state := stack.state(room, bob)
	want := models.RelayedBotView{PlayerID: bobsSeat.PlayerID, TeamColor: 1, DisplayName: "Localhost bot (port 41415) #2", Port: 41415}
	if len(state.RelayedBots) != 1 || state.RelayedBots[0] != want {
		t.Fatalf("Bob's relayed bots = %+v, want %+v", state.RelayedBots, want)
	}
}
