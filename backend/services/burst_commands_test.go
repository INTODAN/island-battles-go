package services_test

import (
	"context"
	"encoding/json"
	"testing"

	"islandBattles/backend/models"
	"islandBattles/backend/services"
)

func burst(castleID, amount uint) models.PublicGenerationCommand {
	raw, _ := json.Marshal(map[string]uint{"castle_id": castleID, "soldier_amount": amount})
	return models.PublicGenerationCommand{CommandType: models.BurstCastle, Parameters: raw}
}

// Castle 0 belongs to the first player's team and castle 1 to the other team;
// each starts with 30 soldiers at burst_potential level 1, a potential of 0.
func TestBurstRejectionsCarryProblemTypes(t *testing.T) {
	fixture := newTurnFixture(t)
	ctx := context.Background()
	raw := func(parameters string) models.PublicGenerationCommand {
		return models.PublicGenerationCommand{CommandType: models.BurstCastle, Parameters: json.RawMessage(parameters)}
	}
	potential := upgrade(0, models.BurstPotentialUpgrade)
	cases := []struct {
		name        string
		commands    []models.PublicGenerationCommand
		status      int
		problemType string
		detail      string
	}{
		{"missing castle_id", []models.PublicGenerationCommand{raw(`{"soldier_amount":1}`)}, 422, "", "burst_castle parameters"},
		{"missing soldier_amount", []models.PublicGenerationCommand{raw(`{"castle_id":0}`)}, 422, "", "burst_castle parameters"},
		{"no soldiers", []models.PublicGenerationCommand{burst(0, 0)}, 422, "", "burst_castle parameters"},
		{"negative soldiers", []models.PublicGenerationCommand{raw(`{"castle_id":0,"soldier_amount":-1}`)}, 422, "", "burst_castle parameters"},
		{"extra key", []models.PublicGenerationCommand{raw(`{"castle_id":0,"soldier_amount":1,"turns":2}`)}, 422, "", "burst_castle parameters"},
		{"missing castle", []models.PublicGenerationCommand{burst(123, 1)}, 422, services.CastleNotFoundProblemType, "castle 123"},
		{"enemy castle", []models.PublicGenerationCommand{burst(1, 1)}, 403, services.CastleNotOwnedProblemType, "castle 1 belongs to team 2"},
		{"level 1 has no potential", []models.PublicGenerationCommand{burst(0, 1)}, 409, services.BurstAbovePotentialProblemType, "burst potential of 0"},
		{"above the potential", []models.PublicGenerationCommand{potential, burst(0, 5)}, 409, services.BurstAbovePotentialProblemType, "burst potential of 4 at burst_potential level 2; a burst of 5"},
		{"before the upgrade", []models.PublicGenerationCommand{burst(0, 4), potential}, 409, services.BurstAbovePotentialProblemType, "burst potential of 0"},
		{"twice in one batch", []models.PublicGenerationCommand{potential, burst(0, 2), burst(0, 2)}, 409, services.BurstUnavailableProblemType, "castle 0 already bursts this turn"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			_, err := fixture.service.Register(ctx, fixture.players[0], batch(fixture.state, false, test.commands...))
			assertProblem(t, err, test.status, test.problemType, test.detail)
		})
	}
	// Every rejected batch was dropped whole.
	assertTurnCounts(t, fixture, 1, 0, 0, 2)
}

// A burst is public: it is pending in the next generation, then paid out with
// the production and owed, and the castle can't burst again until the debt is
// repaid.
func TestABurstIsPendingThenOwed(t *testing.T) {
	fixture := newTurnFixture(t)
	ctx := context.Background()
	alice, bob := fixture.players[0], fixture.players[1]
	state := fixture.state
	// The burst counts the upgrade of an earlier batch, and a second burst in
	// a later batch of the turn is refused.
	if _, err := fixture.service.Register(ctx, alice, batch(state, false, upgrade(0, models.BurstPotentialUpgrade))); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.service.Register(ctx, alice, batch(state, false, burst(0, 4))); err != nil {
		t.Fatal(err)
	}
	_, err := fixture.service.Register(ctx, alice, batch(state, false, burst(0, 1)))
	assertProblem(t, err, 409, services.BurstUnavailableProblemType, "castle 0 already bursts this turn")
	completeTurn := func(state models.GenerationState) models.GenerationState {
		t.Helper()
		if _, err := fixture.service.Register(ctx, alice, batch(state, true)); err != nil {
			t.Fatal(err)
		}
		next, err := fixture.service.Register(ctx, bob, batch(state, true))
		if err != nil {
			t.Fatal(err)
		}
		return next
	}
	castle := func(state models.GenerationState) models.CastleView {
		return state.Generation.Castles[0]
	}
	// Bob sees Alice's pending burst. The castle paid 10 and produced 1.
	state = completeTurn(state)
	if got := castle(state); got.Soldiers != 21 || got.PendingBurst != 4 || got.BurstDebt != 0 {
		t.Fatalf("after the burst: %+v", got)
	}
	_, err = fixture.service.Register(ctx, alice, batch(state, false, burst(0, 1)))
	assertProblem(t, err, 409, services.BurstUnavailableProblemType, "castle 0 already has a pending burst of 4")
	// The production and the burst arrive together, and the burst is owed.
	state = completeTurn(state)
	if got := castle(state); got.Soldiers != 26 || got.PendingBurst != 0 || got.BurstDebt != 4 {
		t.Fatalf("after the payout: %+v", got)
	}
	_, err = fixture.service.Register(ctx, alice, batch(state, false, burst(0, 1)))
	assertProblem(t, err, 409, services.BurstUnavailableProblemType, "castle 0 still owes 4 soldiers")
	// Four turns of production repay the debt, and the castle grows again.
	for range 4 {
		state = completeTurn(state)
	}
	if got := castle(state); got.Soldiers != 26 || got.BurstDebt != 0 {
		t.Fatalf("after repaying: %+v", got)
	}
	if _, err := fixture.service.Register(ctx, alice, batch(state, false, burst(0, 4))); err != nil {
		t.Fatalf("a castle without debt can't burst again: %v", err)
	}
	state = completeTurn(state)
	if got := castle(state); got.Soldiers != 27 || got.PendingBurst != 4 {
		t.Fatalf("after the second burst: %+v", got)
	}
}
