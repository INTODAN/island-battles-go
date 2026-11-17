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

// A burst ordered in turn T arrives in the very next generation, T+1, on top
// of that generation's production, and becomes the debt. Later production
// repays the debt before the castle grows, and what is left over grows it.
// Once the debt is repaid, the castle can burst again.
func TestABurstArrivesInTheNextGenerationAndIsRepaid(t *testing.T) {
	upgrades := models.Upgrades{models.GenerationAmountUpgrade: 3, models.BurstPotentialUpgrade: 3}
	state := newState(10, simulation.Castle{TeamColor: 1, Upgrades: upgrades})
	burst := simulation.BurstCastleParams{PersistentCastleID: 0, SoldierAmount: 8}
	type castleState struct{ soldiers, debt uint }
	want := []castleState{
		{21, 8}, // the burst's turn: production 3 plus the burst of 8, which becomes the debt
		{21, 5}, // the production repays 3
		{21, 2}, // and 3 more
		{22, 0}, // 2 repay the rest, and 1 is left over
		{25, 0}, // the castle grows again
		{36, 8}, // and bursts again
	}
	for turn, expected := range want {
		var commands []simulation.Command
		if turn == 0 || turn == 5 {
			commands = append(commands, burst)
		}
		state = playTurn(t, state, commands...)
		castle := state.Castles[0]
		if got := (castleState{castle.Soldiers, castle.BurstDebt}); got != expected || castle.OrderedBurst != 0 {
			t.Fatalf("generation %d: got %+v ordered %d, want %+v", turn+1, got, castle.OrderedBurst, expected)
		}
	}
}

// A castle can't burst nothing, above its burst potential, while it has debt,
// or twice in one turn.
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
	for name, test := range map[string]struct {
		state    simulation.GameState
		commands []simulation.Command
		want     error
	}{
		"nothing":             {fresh(), []simulation.Command{burst(0)}, models.ErrInvalidBurst},
		"above the potential": {fresh(), append([]simulation.Command{burst(5)}, endOfTurn...), models.ErrBurstAbovePotential},
		"level 1":             {newState(10, simulation.Castle{TeamColor: 1}), append([]simulation.Command{burst(1)}, endOfTurn...), models.ErrBurstAbovePotential},
		"in debt":             {inDebt, []simulation.Command{burst(1)}, models.ErrBurstUnavailable},
		"twice":               {fresh(), []simulation.Command{burst(1), burst(1)}, models.ErrBurstUnavailable},
	} {
		if _, _, err := simulation.Simulate(test.state, test.commands); !errors.Is(err, test.want) {
			t.Errorf("%s: got %v, want %v", name, err, test.want)
		}
	}
	if _, _, err := simulation.Simulate(fresh(), []simulation.Command{simulation.BurstCastleParams{PersistentCastleID: 7, SoldierAmount: 1}}); err == nil {
		t.Error("a missing castle bursts")
	}
	// The debt left by a burst refuses the next turn's burst.
	state := playTurn(t, fresh(), burst(4))
	if _, _, err := simulation.Simulate(state, []simulation.Command{burst(1)}); !errors.Is(err, models.ErrBurstUnavailable) {
		t.Errorf("a castle bursts in the generation after its burst: %v", err)
	}
}

// The burst potential counts every upgrade of the turn, whichever order the
// commands run in.
func TestABurstCountsTheTurnsUpgrades(t *testing.T) {
	upgrade := simulation.UpgradeCastleParams{PersistentCastleID: 0, UpgradeType: models.BurstPotentialUpgrade}
	burst := simulation.BurstCastleParams{PersistentCastleID: 0, SoldierAmount: 4}
	for _, commands := range [][]simulation.Command{{upgrade, burst}, {burst, upgrade}} {
		state := playTurn(t, newState(10, simulation.Castle{TeamColor: 1}), commands...)
		// The upgrade costs 10; the castle produces 1 and gets the burst of 4.
		if castle := state.Castles[0]; castle.BurstDebt != 4 || castle.Soldiers != 5 {
			t.Fatalf("got %+v", castle)
		}
	}
}

// A burst arrives with the turn's production, before the turn's arrivals, so
// it defends the castle in the same turn.
func TestABurstDefendsInItsTurn(t *testing.T) {
	state := captureState()
	state.Castles[1].Upgrades[models.BurstPotentialUpgrade] = 2
	// 104 attackers beat the 101 soldiers the castle has after its
	// production, but not the 105 it has with a burst of 4.
	placeGroup(&state, 1, 104, 3)
	state = playTurn(t, state, simulation.BurstCastleParams{PersistentCastleID: 1, SoldierAmount: 4})
	if castle := state.Castles[1]; castle.TeamColor != 2 || castle.Soldiers != 1 || castle.BurstDebt != 4 {
		t.Fatalf("got %+v", castle)
	}
}

// A capture clears the castle's debt, also the debt of a burst that arrived
// in the turn of the capture, and the new owner's castle grows from the next
// turn on.
func TestACaptureClearsTheDebt(t *testing.T) {
	state := captureState()
	state.Castles[1].BurstDebt = 6
	placeGroup(&state, 1, 200, 3)
	state = playTurn(t, state)
	// The production repays 1 of the debt; 200 attackers beat 100.
	if castle := state.Castles[1]; castle.TeamColor != 1 || castle.Soldiers != 100 || castle.BurstDebt != 0 {
		t.Fatalf("after the capture of a castle in debt: %+v", castle)
	}
	if castle := playTurn(t, state).Castles[1]; castle.Soldiers != 101 || castle.BurstDebt != 0 {
		t.Fatalf("the captured castle doesn't grow: %+v", castle)
	}

	state = captureState()
	state.Castles[1].Upgrades[models.BurstPotentialUpgrade] = 2
	placeGroup(&state, 1, 200, 3)
	state = playTurn(t, state, simulation.BurstCastleParams{PersistentCastleID: 1, SoldierAmount: 4})
	// 100, plus 1 produced and the burst of 4, against 200 attackers.
	if castle := state.Castles[1]; castle.TeamColor != 1 || castle.Soldiers != 95 || castle.BurstDebt != 0 || castle.OrderedBurst != 0 {
		t.Fatalf("the previous owner's burst left a debt after the capture: %+v", castle)
	}
	if castle := playTurn(t, state).Castles[1]; castle.Soldiers != 96 || castle.BurstDebt != 0 {
		t.Fatalf("the captured castle doesn't grow: %+v", castle)
	}
}
