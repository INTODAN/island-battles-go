package simulation

import (
	"islandBattles/backend/models"
	"math"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
)

func TestCalculateClashPositions(t *testing.T) {
	tests := []struct {
		name            string
		positions       []soldierGroupMoveDelta
		expectedClashes int // number of distinct clash points
	}{
		{
			name:            "No soldier groups",
			positions:       []soldierGroupMoveDelta{},
			expectedClashes: 0,
		},
		{
			name: "Head-on collision",
			positions: []soldierGroupMoveDelta{
				{
					Start: pgtype.Point{P: pgtype.Vec2{X: 0, Y: 0}, Valid: true},
					End:   pgtype.Point{P: pgtype.Vec2{X: 2, Y: 0}, Valid: true},
					SoldierGroup: &SoldierGroup{
						Soldiers: 10, TeamColor: 1,
					},
				},
				{
					Start: pgtype.Point{P: pgtype.Vec2{X: 2, Y: 0}, Valid: true},
					End:   pgtype.Point{P: pgtype.Vec2{X: 0, Y: 0}, Valid: true},
					SoldierGroup: &SoldierGroup{
						Soldiers: 10, TeamColor: 2,
					},
				},
			},
			expectedClashes: 1,
		},
		{
			name: "Same team - no collision",
			positions: []soldierGroupMoveDelta{
				{
					Start: pgtype.Point{P: pgtype.Vec2{X: 0, Y: 0}, Valid: true},
					End:   pgtype.Point{P: pgtype.Vec2{X: 2, Y: 0}, Valid: true},
					SoldierGroup: &SoldierGroup{
						Soldiers: 10, TeamColor: 1,
					},
				},
				{
					Start: pgtype.Point{P: pgtype.Vec2{X: 2, Y: 0}, Valid: true},
					End:   pgtype.Point{P: pgtype.Vec2{X: 0, Y: 0}, Valid: true},
					SoldierGroup: &SoldierGroup{
						Soldiers: 10, TeamColor: 1,
					},
				},
			},
			expectedClashes: 0,
		},
		{
			name: "Crossing paths but at different times",
			positions: []soldierGroupMoveDelta{
				{
					Start: pgtype.Point{P: pgtype.Vec2{X: 0, Y: 0}, Valid: true},
					End:   pgtype.Point{P: pgtype.Vec2{X: 10, Y: 10}, Valid: true},
					SoldierGroup: &SoldierGroup{
						Soldiers: 10, TeamColor: 1,
					},
				},
				{
					Start: pgtype.Point{P: pgtype.Vec2{X: 0, Y: 10}, Valid: true},
					End:   pgtype.Point{P: pgtype.Vec2{X: 10, Y: 11}, Valid: true}, // Moves mostly horizontal
					SoldierGroup: &SoldierGroup{
						Soldiers: 10, TeamColor: 2,
					},
				},
			},
			expectedClashes: 0,
		},
		{
			name: "Orthogonal collision at (5,5)",
			positions: []soldierGroupMoveDelta{
				{
					Start: pgtype.Point{P: pgtype.Vec2{X: 0, Y: 5}, Valid: true},
					End:   pgtype.Point{P: pgtype.Vec2{X: 10, Y: 5}, Valid: true},
					SoldierGroup: &SoldierGroup{
						Soldiers: 10, TeamColor: 1,
					},
				},
				{
					Start: pgtype.Point{P: pgtype.Vec2{X: 5, Y: 0}, Valid: true},
					End:   pgtype.Point{P: pgtype.Vec2{X: 5, Y: 10}, Valid: true},
					SoldierGroup: &SoldierGroup{
						Soldiers: 10, TeamColor: 2,
					},
				},
			},
			expectedClashes: 1,
		},
		{
			name: "Never meeting, traveling same vector distance",
			positions: []soldierGroupMoveDelta{
				{
					Start: pgtype.Point{P: pgtype.Vec2{X: 0, Y: 5}, Valid: true},
					End:   pgtype.Point{P: pgtype.Vec2{X: 10, Y: 5}, Valid: true},
					SoldierGroup: &SoldierGroup{
						Soldiers: 10, TeamColor: 1,
					},
				},
				{
					Start: pgtype.Point{P: pgtype.Vec2{X: 0, Y: 0}, Valid: true},
					End:   pgtype.Point{P: pgtype.Vec2{X: 10, Y: 0}, Valid: true},
					SoldierGroup: &SoldierGroup{
						Soldiers: 10, TeamColor: 2,
					},
				},
			},
			expectedClashes: 0,
		},
	}

	for _, testCase := range tests {
		t.Run(
			testCase.name, func(t *testing.T) {
				clashes, err := calculateClashPositions(testCase.positions)
				if err != nil {
					t.Fatalf("Unexpected error: %v", err)
				}

				if len(clashes) != testCase.expectedClashes {
					t.Errorf("Expected %d clash points, got %d", testCase.expectedClashes, len(clashes))
				}

				if testCase.name == "Head-on collision" {
					clash := clashes[0]
					left, right := clash.Participants[0].Position.P, clash.Participants[1].Position.P
					if math.Abs(math.Hypot(left.X-right.X, left.Y-right.Y)-models.DefaultClashRadius) > 1e-9 {
						t.Fatalf("groups must make contact one clash radius apart: %v", clash)
					}
				}
			},
		)
	}
}

func TestClashesWithCoordinateRounding(t *testing.T) {
	// These are the turn-5 positions from the circular demo map. The groups
	// cross halfway through the next turn despite negligible Y-axis roundoff.
	for _, vertical := range []bool{false, true} {
		for _, offset := range []float64{0, 0.5} {
			point := func(x, y float64) pgtype.Point {
				if vertical {
					x, y = y, x
				}
				return pgtype.Point{P: pgtype.Vec2{X: x, Y: y}, Valid: true}
			}
			positions := []soldierGroupMoveDelta{
				{Start: point(0.5, -2.143131898507865e-16), End: point(1, -3.0616169978683787e-16), SoldierGroup: &SoldierGroup{Soldiers: 10, TeamColor: 1}},
				{Start: point(1, -3.0616169978683787e-16+offset), End: point(0.5, -2.143131898507865e-16+offset), SoldierGroup: &SoldierGroup{Soldiers: 10, TeamColor: 2}},
			}
			clashes, err := calculateClashPositions(positions)
			if err != nil {
				t.Fatal(err)
			}
			if offset == 0 {
				if len(clashes) != 1 {
					t.Fatalf("vertical=%v: expected one clash, got %v", vertical, clashes)
				}
				for _, clash := range clashes {
					if len(clash.Participants) != 2 {
						t.Fatalf("expected both groups in clash: %v", clash)
					}
				}
			} else if len(clashes) != 0 {
				t.Fatalf("vertical=%v: separate parallel paths must not clash", vertical)
			}
		}
	}
}

func TestRadiusContactDuringMovement(t *testing.T) {
	point := func(x, y float64) pgtype.Point { return pgtype.Point{P: pgtype.Vec2{X: x, Y: y}, Valid: true} }
	for _, scenario := range []struct {
		name                                     string
		leftStart, leftEnd, rightStart, rightEnd pgtype.Point
		leftRadius, rightRadius                  float64
		sameTeam                                 bool
		clashes                                  bool
		time                                     float64
	}{
		{name: "inside at start", leftStart: point(0, 0), leftEnd: point(-1, 0), rightStart: point(.2, 0), rightEnd: point(1, 0), clashes: true},
		{name: "inclusive boundary", leftStart: point(0, 0), leftEnd: point(0, 0), rightStart: point(.25, 0), rightEnd: point(.25, 0), clashes: true},
		{name: "overlapping enemies", leftStart: point(0, 0), leftEnd: point(1, 0), rightStart: point(0, 0), rightEnd: point(-1, 0), clashes: true},
		{name: "same team ignored", leftStart: point(0, 0), leftEnd: point(1, 0), rightStart: point(.1, 0), rightEnd: point(-1, 0), sameTeam: true},
		{name: "radii are not added", leftStart: point(0, 0), leftEnd: point(1, 0), rightStart: point(0, .4), rightEnd: point(1, .4)},
		{name: "larger left radius", leftStart: point(0, 0), leftEnd: point(0, 0), rightStart: point(.6, 0), rightEnd: point(.6, 0), leftRadius: .75, rightRadius: .25, clashes: true},
		{name: "larger right radius", leftStart: point(0, 0), leftEnd: point(0, 0), rightStart: point(.6, 0), rightEnd: point(.6, 0), leftRadius: .25, rightRadius: .75, clashes: true},
		{name: "pass through within turn", leftStart: point(0, 0), leftEnd: point(2, 0), rightStart: point(2, 0), rightEnd: point(0, 0), clashes: true, time: .4375},
		{name: "tangent paths", leftStart: point(-1, 0), leftEnd: point(1, 0), rightStart: point(0, .25), rightEnd: point(0, .25), clashes: true, time: .5},
		{name: "outside tangent", leftStart: point(-1, 0), leftEnd: point(1, 0), rightStart: point(0, .2501), rightEnd: point(0, .2501)},
		{name: "moving apart", leftStart: point(0, 0), leftEnd: point(-1, 0), rightStart: point(1, 0), rightEnd: point(2, 0)},
		{name: "contact at end", leftStart: point(0, 0), leftEnd: point(.5, 0), rightStart: point(1.25, 0), rightEnd: point(.75, 0), clashes: true, time: 1},
		{name: "contact next turn", leftStart: point(0, 0), leftEnd: point(.5, 0), rightStart: point(1.3, 0), rightEnd: point(.8, 0)},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			rightTeam := uint(2)
			if scenario.sameTeam {
				rightTeam = 1
			}
			movements := []soldierGroupMoveDelta{
				{Start: scenario.leftStart, End: scenario.leftEnd, SoldierGroup: &SoldierGroup{Soldiers: 10, TeamColor: 1, ClashRadius: scenario.leftRadius}},
				{Start: scenario.rightStart, End: scenario.rightEnd, SoldierGroup: &SoldierGroup{Soldiers: 10, TeamColor: rightTeam, ClashRadius: scenario.rightRadius}},
			}
			clashes, err := calculateClashPositions(movements)
			if err != nil {
				t.Fatal(err)
			}
			if scenario.clashes {
				if len(clashes) != 1 || len(clashes[0].Participants) != 2 || math.Abs(clashes[0].Time-scenario.time) > 1e-9 {
					t.Fatalf("unexpected clashes: %+v", clashes)
				}
			} else if len(clashes) != 0 {
				t.Fatalf("unexpected clash: %+v", clashes)
			}
		})
	}
}

func TestSimultaneousClashesAndEarlierContacts(t *testing.T) {
	movement := func(start, end float64, team uint) soldierGroupMoveDelta {
		return soldierGroupMoveDelta{Start: pgtype.Point{P: pgtype.Vec2{X: start}, Valid: true}, End: pgtype.Point{P: pgtype.Vec2{X: end}, Valid: true}, SoldierGroup: &SoldierGroup{Soldiers: 10, TeamColor: team}}
	}
	clashes, err := calculateClashPositions([]soldierGroupMoveDelta{movement(-.25, -.25, 1), movement(0, 0, 2), movement(.25, .25, 1)})
	if err != nil || len(clashes) != 1 || len(clashes[0].Participants) != 3 {
		t.Fatalf("simultaneous contacts must share a clash: %+v, %v", clashes, err)
	}
	clashes, err = calculateClashPositions([]soldierGroupMoveDelta{movement(0, 2, 1), movement(.25, .25, 2), movement(1, 1, 2)})
	if err != nil || len(clashes) != 1 || len(clashes[0].Participants) != 2 {
		t.Fatalf("a destroyed group must not clash farther along its original path: %+v, %v", clashes, err)
	}
}
