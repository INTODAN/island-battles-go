package calculations

import (
	"math"
	"testing"

	"islandBattles/backend/dal/commandHandlers/move_soldier_groups/internal"
	"islandBattles/backend/models"

	"github.com/jackc/pgx/v5/pgtype"
)

func clashMovement(start, end float64, team, soldiers, persistentID uint) internal.SoldierGroupMoveDelta {
	return internal.SoldierGroupMoveDelta{
		Start:        pgtype.Point{P: pgtype.Vec2{X: start}, Valid: true},
		End:          pgtype.Point{P: pgtype.Vec2{X: end}, Valid: true},
		SoldierGroup: &models.SoldierGroup{TeamColor: team, PersistentSoldierGroupID: persistentID, SoldierGroupFields: models.SoldierGroupFields{SoldierAmount: soldiers}},
	}
}

func TestSurvivorFightsEveryLaterContact(t *testing.T) {
	movements := []internal.SoldierGroupMoveDelta{clashMovement(0, 2, 1, 10, 0), clashMovement(.25, .25, 2, 1, 1), clashMovement(1, 1, 2, 3, 2)}
	clashes, err := CalculateClashPositions(movements)
	if err != nil || len(clashes) != 2 {
		t.Fatalf("clashes=%+v err=%v", clashes, err)
	}
	if clashes[0].Participants[0].SoldiersAfter != 9 || clashes[1].Participants[0].SoldiersAfter != 6 {
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
	for level, speed := range []float64{.5, 1, 1.5, 2} {
		group := models.SoldierGroup{
			Position:          pgtype.Point{P: pgtype.Vec2{X: 0}, Valid: true},
			DestinationCastle: models.Castle{CastleFields: models.CastleFields{Position: pgtype.Point{P: pgtype.Vec2{X: .2}, Valid: true}}},
			Upgrades:          models.Upgrades{models.SpeedUpgrade: level + 1},
		}
		movements, err := CalculateInitialPositions([]models.SoldierGroup{group})
		if err != nil {
			t.Fatal(err)
		}
		movement := movements[0]
		arrival := .2 / speed
		if movement.ArrivalTime == nil || math.Abs(*movement.ArrivalTime-arrival) > epsilon || math.Abs(positionAt(movement, arrival/2).P.X-.1) > epsilon {
			t.Fatalf("level %d: short trip used the wrong speed: %+v", level+1, movement)
		}
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
			expected := []uint{8, 10, 0, 0}[participant.SoldierGroup.PersistentSoldierGroupID]
			if participant.SoldiersAfter != expected {
				t.Fatalf("input order changed outcome: %+v", clashes[0])
			}
		}
	}
}

func TestClashStopThresholdAndMidpoint(t *testing.T) {
	for _, scenario := range []struct {
		name        string
		left, right uint
		stop        bool
	}{
		{"less than half", 10, 4, false},
		{"less than half reversed", 4, 10, false},
		{"exactly half", 10, 5, true},
		{"exactly half reversed", 5, 10, true},
		{"equal", 10, 10, true},
		{"odd below half", 9, 4, false},
		{"odd above half", 9, 5, true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			movements := []internal.SoldierGroupMoveDelta{clashMovement(0, 2, 1, scenario.left, 0), clashMovement(2, 0, 2, scenario.right, 1)}
			clashes, err := CalculateClashPositions(movements)
			if err != nil || len(clashes) != 1 {
				t.Fatalf("clashes=%+v err=%v", clashes, err)
			}
			expectedLeft, expectedRight := 2.0, 0.0
			if scenario.stop {
				expectedLeft, expectedRight = 1, 1
			}
			if movements[0].End.P.X != expectedLeft || movements[1].End.P.X != expectedRight {
				t.Fatalf("wrong final positions: %+v", movements)
			}
			expectedPause := uint(0)
			if scenario.stop {
				expectedPause = 2
			}
			if movements[0].PauseGenerations != expectedPause || movements[1].PauseGenerations != expectedPause {
				t.Fatal("default stop must include the clash generation and two more")
			}
		})
	}
}

func TestClashRechecksPathsAfterStopping(t *testing.T) {
	for _, laterMoves := range []bool{false, true} {
		laterEnd := 1.0
		if laterMoves {
			laterEnd = 0
		}
		movements := []internal.SoldierGroupMoveDelta{clashMovement(0, 2, 1, 20, 0), clashMovement(.25, .25, 2, 10, 1), clashMovement(1, laterEnd, 2, 1, 2)}
		clashes, err := CalculateClashPositions(movements)
		expectedClashes := 1
		if laterMoves {
			expectedClashes = 2
		}
		if err != nil || len(clashes) != expectedClashes {
			t.Fatalf("laterMoves=%v: stale paths used: %+v err=%v", laterMoves, clashes, err)
		}
		if movements[0].End.P.X != .125 {
			t.Fatal("small later contact moved the paused survivor")
		}
		if laterMoves && math.Abs(clashes[1].Time-.625) > epsilon {
			t.Fatalf("wrong later contact time: %v", clashes[1].Time)
		}
	}
}

func TestClashPauseUsesEachGroupUpgrade(t *testing.T) {
	movements := []internal.SoldierGroupMoveDelta{clashMovement(0, 1, 1, 20, 0), clashMovement(0, 1, 1, 10, 1), clashMovement(0, -1, 2, 15, 2)}
	movements[1].SoldierGroup.Upgrades = models.Upgrades{models.ClashStopGenerationsUpgrade: 3}
	clashes, err := CalculateClashPositions(movements)
	if err != nil || len(clashes) != 1 {
		t.Fatalf("clashes=%+v err=%v", clashes, err)
	}
	if movements[0].PauseGenerations != 2 || movements[1].PauseGenerations != 0 {
		t.Fatal("each survivor must use its own upgrade snapshot")
	}
	if clashes[0].Participants[0].SoldiersAfter != 5 || clashes[0].Participants[1].SoldiersAfter != 10 {
		t.Fatal("both allied groups must survive this clash")
	}
}

func TestResolvingAClashDoesNotChangeWhetherItStops(t *testing.T) {
	clashParticipant := func(team, soldiers, persistentID uint) internal.ClashParticipant {
		return internal.ClashParticipant{SoldierGroup: &models.SoldierGroup{TeamColor: team, PersistentSoldierGroupID: persistentID}, SoldiersBefore: soldiers}
	}
	for _, scenario := range []struct {
		name         string
		participants []internal.ClashParticipant
		stop         bool
		after        map[uint]uint // SoldiersAfter by persistent group ID
	}{
		{"less than half", []internal.ClashParticipant{clashParticipant(1, 10, 0), clashParticipant(2, 4, 1)}, false, map[uint]uint{0: 6, 1: 0}},
		{"exactly half", []internal.ClashParticipant{clashParticipant(1, 10, 0), clashParticipant(2, 5, 1)}, true, map[uint]uint{0: 5, 1: 0}},
		{"equal", []internal.ClashParticipant{clashParticipant(1, 10, 0), clashParticipant(2, 10, 1)}, true, map[uint]uint{0: 0, 1: 0}},
		{"odd below half", []internal.ClashParticipant{clashParticipant(1, 9, 0), clashParticipant(2, 4, 1)}, false, map[uint]uint{0: 5, 1: 0}},
		{"odd above half", []internal.ClashParticipant{clashParticipant(1, 9, 0), clashParticipant(2, 5, 1)}, true, map[uint]uint{0: 4, 1: 0}},
		{"allies out of order", []internal.ClashParticipant{clashParticipant(2, 15, 2), clashParticipant(1, 10, 1), clashParticipant(1, 20, 0)}, true, map[uint]uint{0: 5, 1: 10, 2: 0}},
		{"three uneven teams", []internal.ClashParticipant{clashParticipant(1, 100, 0), clashParticipant(2, 20, 1), clashParticipant(3, 4, 2)}, false, map[uint]uint{0: 76, 1: 0, 2: 0}},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			clash := internal.Clash{Participants: scenario.participants}
			soldiersBefore := map[*models.SoldierGroup]uint{}
			for _, participant := range clash.Participants {
				soldiersBefore[participant.SoldierGroup] = participant.SoldiersBefore
			}
			stopBeforeResolution := shouldStop(&clash)
			resolveClash(&clash)
			stopAfterResolution := shouldStop(&clash)
			if stopBeforeResolution != scenario.stop || stopAfterResolution != scenario.stop {
				t.Fatalf("stop before resolution=%v, after=%v; want %v", stopBeforeResolution, stopAfterResolution, scenario.stop)
			}
			for _, participant := range clash.Participants {
				if participant.SoldiersBefore != soldiersBefore[participant.SoldierGroup] {
					t.Fatalf("resolution changed the count before contact: %+v", clash.Participants)
				}
				if want := scenario.after[participant.SoldierGroup.PersistentSoldierGroupID]; participant.SoldiersAfter != want {
					t.Fatalf("group %d: SoldiersAfter=%d, want %d", participant.SoldierGroup.PersistentSoldierGroupID, participant.SoldiersAfter, want)
				}
			}
		})
	}
}

func TestClashEventsMapsParticipantsAndNumbersClashes(t *testing.T) {
	group := func(team, persistentID uint) *models.SoldierGroup {
		return &models.SoldierGroup{TeamColor: team, PersistentSoldierGroupID: persistentID}
	}
	participant := func(group *models.SoldierGroup, x, y float64, before, after uint) internal.ClashParticipant {
		return internal.ClashParticipant{SoldierGroup: group, Position: pgtype.Point{P: pgtype.Vec2{X: x, Y: y}, Valid: true}, SoldiersBefore: before, SoldiersAfter: after}
	}
	events := ClashEvents([]internal.Clash{
		{Time: .25, Participants: []internal.ClashParticipant{participant(group(1, 7), 1, 2, 10, 4), participant(group(2, 9), 3, 4, 6, 0)}},
		{Time: .5, Participants: []internal.ClashParticipant{participant(group(1, 7), 5, 6, 4, 3)}},
	})
	if len(events) != 2 || events[0].ID != 1 || events[1].ID != 2 || events[0].Time != .25 || events[1].Time != .5 {
		t.Fatalf("clash IDs or times wrong: %+v", events)
	}
	first := events[0].Participants
	if len(first) != 2 {
		t.Fatalf("participants: %+v", first)
	}
	if first[0].GroupID != 7 || first[0].TeamColor != 1 || first[0].X != 1 || first[0].Y != 2 || first[0].SoldiersBefore != 10 || first[0].SoldiersAfter != 4 {
		t.Fatalf("first participant: %+v", first[0])
	}
	if first[1].GroupID != 9 || first[1].TeamColor != 2 || first[1].X != 3 || first[1].Y != 4 || first[1].SoldiersBefore != 6 || first[1].SoldiersAfter != 0 {
		t.Fatalf("second participant: %+v", first[1])
	}
	if last := events[1].Participants[0]; last.SoldiersBefore != 4 || last.SoldiersAfter != 3 {
		t.Fatalf("second clash participant: %+v", last)
	}
}
