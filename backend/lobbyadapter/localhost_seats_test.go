package lobbyadapter_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/INTODAN/mpl/domain"
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
	stack.expect(stack.addSeat(room, alice, 1, map[string]any{"bot_localhost_port": "41415"}), http.StatusOK, &seated)
	if seat := seatOn(t, seated, 1); seat.DisplayName != "Localhost bot (port 41415)" {
		t.Fatalf("seat = %+v", seat)
	}
	stack.expect(stack.addSeat(room, alice, 2, map[string]any{"bot_localhost_port": 41416}), http.StatusOK, &seated)
	if seat := seatOn(t, seated, 2); seat.DisplayName != "Localhost bot (port 41416)" {
		t.Fatalf("seat = %+v", seat)
	}

	// Each of the host's localhost bots needs a port of its own, and a port
	// is not a privileged one.
	for _, refused := range []struct {
		settings map[string]any
		detail   string
	}{
		{map[string]any{"bot_localhost_port": 41415}, "port 41415 is already taken by Localhost bot (port 41415)"},
		{map[string]any{"bot_localhost_port": "41414"}, "port 41414 is already taken by the host's own localhost bot"},
		{map[string]any{"bot_localhost_port": 80}, "a localhost bot's port must be from 1024 to 65535"},
		{map[string]any{"bot_localhost_port": 70000}, "a localhost bot's port must be from 1024 to 65535"},
		{map[string]any{"bot_localhost_port": "41 417"}, `{"bot_localhost_port": N} with a port from 1024 to 65535`},
		{map[string]any{"bot_localhost_port": 41417, "bot_level": 1}, `{"bot_localhost_port": N} with a port from 1024 to 65535`},
	} {
		var refusal problem
		stack.expect(stack.addSeat(room, alice, 3, refused.settings), http.StatusUnprocessableEntity, &refusal)
		if refusal.Code != "invalid_seat_settings" || !strings.Contains(refusal.Detail, refused.detail) {
			t.Fatalf("seat %v = %+v", refused.settings, refusal)
		}
	}
	// The first free port is suggested.
	var refusal problem
	stack.expect(stack.addSeat(room, alice, 3, map[string]any{"bot_localhost_port": 41416}), http.StatusUnprocessableEntity, &refusal)
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
