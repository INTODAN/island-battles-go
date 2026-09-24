package calculations

import (
	"math"
	"testing"

	"islandBattles/backend/dal/commandHandlers/move_soldier_groups/internal"
	"islandBattles/backend/models"

	"github.com/jackc/pgx/v5/pgtype"
)

func clashMovement(start, end float64, team, soldiers, logicalID uint) internal.SoldierGroupMoveDelta {
	return internal.SoldierGroupMoveDelta{
		Start:        pgtype.Point{P: pgtype.Vec2{X: start}, Valid: true},
		End:          pgtype.Point{P: pgtype.Vec2{X: end}, Valid: true},
		SoldierGroup: &models.SoldierGroup{TeamColor: team, LogicalSoldierGroupID: logicalID, SoldierGroupFields: models.SoldierGroupFields{SoldierAmount: soldiers}},
	}
}

func TestSurvivorFightsEveryLaterContact(t *testing.T) {
	movements := []internal.SoldierGroupMoveDelta{clashMovement(0, 2, 1, 10, 0), clashMovement(.25, .25, 2, 1, 1), clashMovement(1, 1, 2, 3, 2)}
	clashes, err := CalculateClashPositions(movements)
	if err != nil || len(clashes) != 2 {
		t.Fatalf("clashes=%+v err=%v", clashes, err)
	}
	if clashes[0].Participants[0].RemainingSoldiers != 9 || clashes[1].Participants[0].RemainingSoldiers != 6 {
		t.Fatalf("survivor bypassed an enemy: %+v", clashes)
	}
	if movements[0].SoldierGroup.SoldierAmount != 10 {
		t.Fatal("calculation mutated persisted input")
	}
}

func TestDeadGroupCannotBridgeLaterClash(t *testing.T) {
	movements := []internal.SoldierGroupMoveDelta{
		clashMovement(0, 0, 1, 1, 0),
		clashMovement(.25, .25, 2, 1, 1),
		clashMovement(1, 0, 3, 10, 2),
		clashMovement(-.5, -.5, 1, 10, 3),
	}
	clashes, err := CalculateClashPositions(movements)
	if err != nil || len(clashes) != 1 {
		t.Fatalf("dead groups still participate: %+v, %v", clashes, err)
	}
}

func TestArrivalTakesPriorityOverClash(t *testing.T) {
	for _, scenario := range []struct {
		name                   string
		arrival, enemyPosition float64
		clashes                int
	}{
		{"already arrived beside spawn", 0, 0, 0},
		{"arrival before contact", .5, 1.2, 0},
		{"arrival tied with contact", .5, 1, 0},
		{"contact before arrival", .5, .85, 1},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			incoming := clashMovement(0, .5, 1, 10, 0)
			incoming.ArrivalTime = &scenario.arrival
			enemy := clashMovement(scenario.enemyPosition, scenario.enemyPosition-.5, 2, 1, 1)
			clashes, err := CalculateClashPositions([]internal.SoldierGroupMoveDelta{incoming, enemy})
			if err != nil || len(clashes) != scenario.clashes {
				t.Fatalf("clashes=%+v err=%v", clashes, err)
			}
		})
	}
}

func TestShortTripKeepsItsFullSpeed(t *testing.T) {
	group := models.SoldierGroup{Position: pgtype.Point{P: pgtype.Vec2{X: 0}, Valid: true}, DestinationCastle: models.Castle{CastleFields: models.CastleFields{Position: pgtype.Point{P: pgtype.Vec2{X: .2}, Valid: true}}}}
	movement := CalculateInitialPositions([]models.SoldierGroup{group})[0]
	if movement.ArrivalTime == nil || math.Abs(*movement.ArrivalTime-.4) > epsilon || math.Abs(positionAt(movement, .2).P.X-.1) > epsilon {
		t.Fatalf("short trip slowed down: %+v", movement)
	}
}

func TestZeroSoldiersCannotTriggerClash(t *testing.T) {
	clashes, err := CalculateClashPositions([]internal.SoldierGroupMoveDelta{clashMovement(0, 1, 1, 10, 0), clashMovement(0, 0, 2, 0, 1)})
	if err != nil || len(clashes) != 0 {
		t.Fatalf("empty group caused combat: %+v %v", clashes, err)
	}
}

func TestSimultaneousCombatDoesNotDependOnInputOrder(t *testing.T) {
	groups := []internal.SoldierGroupMoveDelta{clashMovement(0, 0, 1, 20, 0), clashMovement(0, 0, 1, 10, 1), clashMovement(0, 0, 2, 5, 2), clashMovement(0, 0, 3, 7, 3)}
	for rotation := 0; rotation < len(groups); rotation++ {
		movements := append(append([]internal.SoldierGroupMoveDelta{}, groups[rotation:]...), groups[:rotation]...)
		clashes, err := CalculateClashPositions(movements)
		if err != nil || len(clashes) != 1 {
			t.Fatalf("clashes=%+v err=%v", clashes, err)
		}
		for _, participant := range clashes[0].Participants {
			expected := []uint{8, 10, 0, 0}[participant.SoldierGroup.LogicalSoldierGroupID]
			if participant.RemainingSoldiers != expected {
				t.Fatalf("input order changed outcome: %+v", clashes[0])
			}
		}
	}
}
