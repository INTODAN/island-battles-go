package lobbyadapter_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/INTODAN/mpl/domain"
	"islandBattles/backend/models"
	"islandBattles/backend/services"
)

// Once the bot runners have used the month's compute, no game with an
// uploaded bot starts, whether a member's or only a seat's; games with only
// hosted and default bots still do, and getting ready is never refused.
func TestTheComputeGuardRefusesStartsWithUploadedBotsOverBudget(t *testing.T) {
	guard := &services.BotComputeGuard{Meter: services.FixedBotCompute(300_000), BudgetGBSeconds: 300_000,
		Now: func() time.Time { return time.Date(2026, time.December, 31, 23, 59, 0, 0, time.UTC) }}
	stack := newLobbyStackWith(t, services.ServerBots{Enabled: true, Resolver: botHosts, Compute: guard})
	alice := stack.guest("Alice")
	room := stack.teamsRoom(alice, "Red", "Blue", "Green")
	alicePackage := stack.uploadPackage(room, alice, "Alice", "python")
	start := func() *httptest.ResponseRecorder {
		return stack.api.Post(stack.lobbyPath(room)+"/start", alice.header, map[string]any{})
	}
	refused := func(when string) {
		t.Helper()
		var refusal gameRequirement
		stack.expect(start(), http.StatusConflict, &refusal)
		if refusal.Code != "game_requirement" || refusal.Type != services.BotComputeProblemType ||
			refusal.Detail != "Uploaded bots have used this month's free compute. They're back on 1 January." {
			t.Fatalf("%s: start = %+v", when, refusal)
		}
	}

	// The bot pickers read the same.
	var compute models.BotComputeView
	stack.expect(stack.api.Get("/game/bot-compute"), http.StatusOK, &compute)
	if compute != (models.BotComputeView{Month: "2026-12", UsedGBSeconds: 300_000, BudgetGBSeconds: 300_000, Available: false}) {
		t.Fatalf("GET /game/bot-compute = %+v", compute)
	}

	// Alice's own uploaded bot: she gets ready, but the start is refused.
	stack.expect(stack.choose(room, alice, `{"kind":"uploaded","package_id":"`+alicePackage+`"}`), http.StatusOK, nil)
	stack.expect(stack.ready(room, alice), http.StatusOK, nil)
	stack.expect(stack.addSeat(room, alice, 1, map[string]any{"bot_level": 1}), http.StatusOK, nil)
	var seated domain.Snapshot
	stack.expect(stack.addSeat(room, alice, 2, map[string]any{"bot_level": 3}), http.StatusOK, &seated)
	refused("Alice's uploaded bot")

	// Only a seat's uploaded bot is refused too.
	stack.expect(stack.api.Put(stack.lobbyPath(room)+"/ready", alice.header, map[string]any{"ready": false}), http.StatusOK, nil)
	stack.expect(stack.choose(room, alice, `{"kind":"default","level":2}`), http.StatusOK, nil)
	stack.expect(stack.ready(room, alice), http.StatusOK, nil)
	stack.expect(stack.api.Delete(stack.lobbyPath(room)+"/seats/"+seatOn(t, seated, 2).ID, alice.header), http.StatusOK, nil)
	stack.expect(stack.addSeat(room, alice, 2, map[string]any{"bot_package_id": alicePackage}), http.StatusOK, &seated)
	refused("a seat's uploaded bot")

	// With hosted and default bots only, the game starts.
	stack.expect(stack.api.Delete(stack.lobbyPath(room)+"/seats/"+seatOn(t, seated, 2).ID, alice.header), http.StatusOK, nil)
	stack.expect(stack.addSeat(room, alice, 2, map[string]any{"bot_url": "https://seat.example.com/turn"}), http.StatusOK, nil)
	var started domain.Snapshot
	stack.expect(start(), http.StatusOK, &started)
	if started.Lobby.Phase != domain.PhaseStarted {
		t.Fatalf("phase = %s", started.Lobby.Phase)
	}
}

// Under the budget, a game with uploaded bots starts.
func TestTheComputeGuardAllowsStartsUnderBudget(t *testing.T) {
	guard := &services.BotComputeGuard{Meter: services.FixedBotCompute(299_999.9), BudgetGBSeconds: 300_000}
	stack := newLobbyStackWith(t, services.ServerBots{Enabled: true, Resolver: botHosts, Compute: guard})
	alice := stack.guest("Alice")
	room := stack.teamsRoom(alice, "Red", "Blue")
	alicePackage := stack.uploadPackage(room, alice, "Alice", "python")
	stack.expect(stack.choose(room, alice, `{"kind":"uploaded","package_id":"`+alicePackage+`"}`), http.StatusOK, nil)
	stack.expect(stack.ready(room, alice), http.StatusOK, nil)
	stack.expect(stack.addSeat(room, alice, 1, map[string]any{"bot_package_id": alicePackage}), http.StatusOK, nil)
	var compute models.BotComputeView
	stack.expect(stack.api.Get("/game/bot-compute"), http.StatusOK, &compute)
	if !compute.Available {
		t.Fatalf("GET /game/bot-compute = %+v", compute)
	}
	stack.expect(stack.api.Post(stack.lobbyPath(room)+"/start", alice.header, map[string]any{}), http.StatusOK, nil)
}
