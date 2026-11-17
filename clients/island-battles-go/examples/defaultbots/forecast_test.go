package defaultbots

import (
	"testing"

	islandbattles "github.com/INTODAN/island-battles-go/clients/island-battles-go"
)

// forecast must play the simulation's turn: a castle that a team owns grows
// before the turn's arrivals, a neutral one doesn't, and a castle captured by
// an arrival grows from the next turn on.
func TestForecastGrowsOnlyCastlesATeamOwns(t *testing.T) {
	neutral := islandbattles.Castle{ID: 1, TeamColor: islandbattles.NeutralTeamColor, Soldiers: 10}
	if owner, soldiers := (board{}).forecast(neutral, nil, 5); owner != islandbattles.NeutralTeamColor || soldiers != 10 {
		t.Fatalf("a neutral castle keeps its soldiers, got owner %d with %d", owner, soldiers)
	}
	owned := islandbattles.Castle{ID: 2, TeamColor: 1, Soldiers: 10}
	if owner, soldiers := (board{}).forecast(owned, nil, 5); owner != 1 || soldiers != 15 {
		t.Fatalf("a team's castle grows by one a turn, got owner %d with %d", owner, soldiers)
	}
	// Team 2's group lands on turn 3 with 14: the neutral castle has 10 and
	// doesn't grow in that turn, so 4 are left, and it grows from turn 4 on.
	landing := []arrival{{turn: 3, id: 1, team: 2, soldiers: 14}}
	if owner, soldiers := (board{}).forecast(neutral, landing, 3); owner != 2 || soldiers != 4 {
		t.Fatalf("the capture leaves the surplus, got owner %d with %d", owner, soldiers)
	}
	if owner, soldiers := (board{}).forecast(neutral, landing, 5); owner != 2 || soldiers != 6 {
		t.Fatalf("the captured castle grows from the next turn, got owner %d with %d", owner, soldiers)
	}
}

// forecast and garrisonToHold count a castle's production and its burst
// debt, as the simulation plays them, and a capture clears the debt. The
// fixture turn's table has every level.
func TestForecastCountsProductionAndBurstDebt(t *testing.T) {
	view := board{turn: islandbattles.FixtureTurn()}
	// Production 2 and a debt of 3: turn 1 repays 2, turn 2 repays 1 and
	// grows the castle by 1, and turn 3 grows it by 2.
	castle := islandbattles.Castle{ID: 1, TeamColor: 1, Soldiers: 10, BurstDebt: 3, Upgrades: islandbattles.Upgrades{islandbattles.GenerationAmountUpgrade: 2}}
	for turns, want := range []uint{10, 10, 11, 13, 15} {
		if owner, soldiers := view.forecast(castle, nil, turns); owner != 1 || soldiers != want {
			t.Fatalf("after %d turns: owner %d with %d, want %d", turns, owner, soldiers, want)
		}
	}
	// A group of 20 lands on turn 3, when the castle has grown by 3: it
	// holds with 17 of its soldiers.
	landing := []arrival{{turn: 3, id: 1, team: 2, soldiers: 20}}
	if hold := view.garrisonToHold(castle, landing, 1); hold != 17 {
		t.Fatalf("the castle must keep %d, want 17", hold)
	}
	// Team 2 captures it with 7 left on turn 3, and it grows 2 a turn after,
	// with no debt to repay.
	if owner, soldiers := view.forecast(castle, landing, 4); owner != 2 || soldiers != 9 {
		t.Fatalf("after the capture: owner %d with %d", owner, soldiers)
	}
}
