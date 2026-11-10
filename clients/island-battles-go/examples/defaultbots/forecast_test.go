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
	if owner, soldiers := forecast(neutral, nil, 5); owner != islandbattles.NeutralTeamColor || soldiers != 10 {
		t.Fatalf("a neutral castle keeps its soldiers, got owner %d with %d", owner, soldiers)
	}
	owned := islandbattles.Castle{ID: 2, TeamColor: 1, Soldiers: 10}
	if owner, soldiers := forecast(owned, nil, 5); owner != 1 || soldiers != 15 {
		t.Fatalf("a team's castle grows by one a turn, got owner %d with %d", owner, soldiers)
	}
	// Team 2's group lands on turn 3 with 14: the neutral castle has 10 and
	// doesn't grow in that turn, so 4 are left, and it grows from turn 4 on.
	landing := []arrival{{turn: 3, id: 1, team: 2, soldiers: 14}}
	if owner, soldiers := forecast(neutral, landing, 3); owner != 2 || soldiers != 4 {
		t.Fatalf("the capture leaves the surplus, got owner %d with %d", owner, soldiers)
	}
	if owner, soldiers := forecast(neutral, landing, 5); owner != 2 || soldiers != 6 {
		t.Fatalf("the captured castle grows from the next turn, got owner %d with %d", owner, soldiers)
	}
}
