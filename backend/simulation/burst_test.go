package simulation_test

import (
	"errors"
	"testing"

	"islandBattles/backend/models"
	"islandBattles/backend/simulation"
)

// A castle that a team owns produces its generation_amount every turn; a
// neutral one produces nothing at any level.
func TestCastlesProduceTheirGenerationAmount(t *testing.T) {
	for level, production := range []uint{1, 2, 3, 4} {
		upgrades := models.Upgrades{models.GenerationAmountUpgrade: level + 1}
		state := newState(10, simulation.Castle{TeamColor: 1, Upgrades: upgrades}, simulation.Castle{TeamColor: models.NeutralTeamColor, Upgrades: upgrades})
		state = playTurn(t, state)
		state = playTurn(t, state)
		if owned, neutral := state.Castles[0], state.Castles[1]; owned.Soldiers != 10+2*production || neutral.Soldiers != 10 {
			t.Fatalf("level %d: the owned castle has %d and the neutral one %d", level+1, owned.Soldiers, neutral.Soldiers)
		}
	}
}

// A burst ordered this turn is pending in the next generation. The generation
// after adds it on top of the production and makes it the debt, which later
// production repays before the castle grows, with any leftover growing it.
func TestABurstIsPaidOutAndRepaid(t *testing.T) {
	upgrades := models.Upgrades{models.GenerationAmountUpgrade: 3, models.BurstPotentialUpgrade: 3}
	state := newState(10, simulation.Castle{TeamColor: 1, Upgrades: upgrades})
	burst := simulation.BurstCastleParams{PersistentCastleID: 0, SoldierAmount: 8}
	type castleState struct{ soldiers, debt, pending uint }
	want := []castleState{
		{13, 0, 8}, // the burst is ordered: the castle produces 3, and the burst is pending
		{24, 8, 0}, // production 3 plus the burst of 8, which becomes the debt
		{24, 5, 0}, // the production repays 3
		{24, 2, 0}, // and 3 more
		{25, 0, 0}, // 2 repay the rest, and 1 is left over
		{28, 0, 0}, // the castle grows again
	}
	for turn, expected := range want {
		var commands []simulation.Command
		if turn == 0 {
			commands = append(commands, burst)
		}
		state = playTurn(t, state, commands...)
		castle := state.Castles[0]
		if got := (castleState{castle.Soldiers, castle.BurstDebt, castle.PendingBurst}); got != expected || castle.OrderedBurst != 0 {
			t.Fatalf("turn %d: got %+v ordered %d, want %+v", turn+1, got, castle.OrderedBurst, expected)
		}
	}
}

// A castle can't burst nothing, above its burst potential, while it has debt
// or a pending burst, or twice in one turn.
func TestBurstRejections(t *testing.T) {
	upgrades := models.Upgrades{models.BurstPotentialUpgrade: 2}
	fresh := func() simulation.GameState {
		return newState(10, simulation.Castle{TeamColor: 1, Upgrades: upgrades})
	}
	endOfTurn, err := simulation.DecodeCommands(simulation.EndOfTurnCommands())
	if err != nil {
		t.Fatal(err)
	}
	burst := func(amount uint) simulation.Command {
		return simulation.BurstCastleParams{PersistentCastleID: 0, SoldierAmount: amount}
	}
	inDebt := fresh()
	inDebt.Castles[0].BurstDebt = 1
	pending := fresh()
	pending.Castles[0].PendingBurst = 4
	for name, test := range map[string]struct {
		state    simulation.GameState
		commands []simulation.Command
		want     error
	}{
		"nothing":             {fresh(), []simulation.Command{burst(0)}, models.ErrInvalidBurst},
		"above the potential": {fresh(), append([]simulation.Command{burst(5)}, endOfTurn...), models.ErrBurstAbovePotential},
		"level 1":             {newState(10, simulation.Castle{TeamColor: 1}), append([]simulation.Command{burst(1)}, endOfTurn...), models.ErrBurstAbovePotential},
		"in debt":             {inDebt, []simulation.Command{burst(1)}, models.ErrBurstUnavailable},
		"pending":             {pending, []simulation.Command{burst(1)}, models.ErrBurstUnavailable},
		"twice":               {fresh(), []simulation.Command{burst(1), burst(1)}, models.ErrBurstUnavailable},
	} {
		if _, _, err := simulation.Simulate(test.state, test.commands); !errors.Is(err, test.want) {
			t.Errorf("%s: got %v, want %v", name, err, test.want)
		}
	}
	if _, _, err := simulation.Simulate(fresh(), []simulation.Command{simulation.BurstCastleParams{PersistentCastleID: 7, SoldierAmount: 1}}); err == nil {
		t.Error("a missing castle bursts")
	}
}

// The burst potential counts every upgrade of the turn, whichever order the
// commands run in.
func TestABurstCountsTheTurnsUpgrades(t *testing.T) {
	upgrade := simulation.UpgradeCastleParams{PersistentCastleID: 0, UpgradeType: models.BurstPotentialUpgrade}
	burst := simulation.BurstCastleParams{PersistentCastleID: 0, SoldierAmount: 4}
	for _, commands := range [][]simulation.Command{{upgrade, burst}, {burst, upgrade}} {
		state := playTurn(t, newState(10, simulation.Castle{TeamColor: 1}), commands...)
		if castle := state.Castles[0]; castle.PendingBurst != 4 || castle.Soldiers != 1 {
			t.Fatalf("got %+v", castle)
		}
	}
}

// A capture clears the castle's debt and pending burst, and the burst its
// previous owner ordered this turn.
func TestACaptureClearsTheBurst(t *testing.T) {
	for name, prepare := range map[string]func(*simulation.Castle){
		"debt":    func(castle *simulation.Castle) { castle.BurstDebt = 6 },
		"pending": func(castle *simulation.Castle) { castle.PendingBurst = 6 },
	} {
		state := captureState()
		prepare(state.Castles[1])
		placeGroup(&state, 1, 200, 3)
		state = playTurn(t, state)
		castle := state.Castles[1]
		if castle.TeamColor != 1 || castle.BurstDebt != 0 || castle.PendingBurst != 0 {
			t.Fatalf("%s: got %+v", name, castle)
		}
	}
	state := captureState()
	state.Castles[1].Upgrades[models.BurstPotentialUpgrade] = 2
	placeGroup(&state, 1, 200, 3)
	state = playTurn(t, state, simulation.BurstCastleParams{PersistentCastleID: 1, SoldierAmount: 4})
	if castle := state.Castles[1]; castle.TeamColor != 1 || castle.PendingBurst != 0 || castle.OrderedBurst != 0 {
		t.Fatalf("the previous owner's burst survived the capture: %+v", castle)
	}
}
