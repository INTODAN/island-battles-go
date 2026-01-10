package calculations

import (
	"islandBattles/backend/dal/commandHandlers/move_soldier_groups/internal"
	"islandBattles/backend/models"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
)

func TestCalculateClashPositions(t *testing.T) {
	tests := []struct {
		name            string
		positions       []internal.SoldierGroupMoveDelta
		expectedClashes int // number of distinct clash points
	}{
		{
			name:            "No soldier groups",
			positions:       []internal.SoldierGroupMoveDelta{},
			expectedClashes: 0,
		},
		{
			name: "Head-on collision",
			positions: []internal.SoldierGroupMoveDelta{
				{
					Start: pgtype.Point{P: pgtype.Vec2{X: 0, Y: 0}, Valid: true},
					End:   pgtype.Point{P: pgtype.Vec2{X: 2, Y: 0}, Valid: true},
					SoldierGroup: &models.SoldierGroup{
						TeamColor: 1,
					},
				},
				{
					Start: pgtype.Point{P: pgtype.Vec2{X: 2, Y: 0}, Valid: true},
					End:   pgtype.Point{P: pgtype.Vec2{X: 0, Y: 0}, Valid: true},
					SoldierGroup: &models.SoldierGroup{
						TeamColor: 2,
					},
				},
			},
			expectedClashes: 1,
		},
		{
			name: "Same team - no collision",
			positions: []internal.SoldierGroupMoveDelta{
				{
					Start: pgtype.Point{P: pgtype.Vec2{X: 0, Y: 0}, Valid: true},
					End:   pgtype.Point{P: pgtype.Vec2{X: 2, Y: 0}, Valid: true},
					SoldierGroup: &models.SoldierGroup{
						TeamColor: 1,
					},
				},
				{
					Start: pgtype.Point{P: pgtype.Vec2{X: 2, Y: 0}, Valid: true},
					End:   pgtype.Point{P: pgtype.Vec2{X: 0, Y: 0}, Valid: true},
					SoldierGroup: &models.SoldierGroup{
						TeamColor: 1,
					},
				},
			},
			expectedClashes: 0,
		},
		{
			name: "Crossing paths but at different times",
			positions: []internal.SoldierGroupMoveDelta{
				{
					Start: pgtype.Point{P: pgtype.Vec2{X: 0, Y: 0}, Valid: true},
					End:   pgtype.Point{P: pgtype.Vec2{X: 10, Y: 10}, Valid: true},
					SoldierGroup: &models.SoldierGroup{
						TeamColor: 1,
					},
				},
				{
					Start: pgtype.Point{P: pgtype.Vec2{X: 0, Y: 10}, Valid: true},
					End:   pgtype.Point{P: pgtype.Vec2{X: 10, Y: 11}, Valid: true}, // Moves mostly horizontal
					SoldierGroup: &models.SoldierGroup{
						TeamColor: 2,
					},
				},
			},
			expectedClashes: 0,
		},
		{
			name: "Orthogonal collision at (5,5)",
			positions: []internal.SoldierGroupMoveDelta{
				{
					Start: pgtype.Point{P: pgtype.Vec2{X: 0, Y: 5}, Valid: true},
					End:   pgtype.Point{P: pgtype.Vec2{X: 10, Y: 5}, Valid: true},
					SoldierGroup: &models.SoldierGroup{
						TeamColor: 1,
					},
				},
				{
					Start: pgtype.Point{P: pgtype.Vec2{X: 5, Y: 0}, Valid: true},
					End:   pgtype.Point{P: pgtype.Vec2{X: 5, Y: 10}, Valid: true},
					SoldierGroup: &models.SoldierGroup{
						TeamColor: 2,
					},
				},
			},
			expectedClashes: 1,
		},
		{
			name: "Never meeting, traveling same vector distance",
			positions: []internal.SoldierGroupMoveDelta{
				{
					Start: pgtype.Point{P: pgtype.Vec2{X: 0, Y: 5}, Valid: true},
					End:   pgtype.Point{P: pgtype.Vec2{X: 10, Y: 5}, Valid: true},
					SoldierGroup: &models.SoldierGroup{
						TeamColor: 1,
					},
				},
				{
					Start: pgtype.Point{P: pgtype.Vec2{X: 0, Y: 0}, Valid: true},
					End:   pgtype.Point{P: pgtype.Vec2{X: 10, Y: 0}, Valid: true},
					SoldierGroup: &models.SoldierGroup{
						TeamColor: 2,
					},
				},
			},
			expectedClashes: 0,
		},
	}

	for _, testCase := range tests {
		t.Run(
			testCase.name, func(t *testing.T) {
				clashes, err := CalculateClashPositions(testCase.positions)
				if err != nil {
					t.Fatalf("Unexpected error: %v", err)
				}

				if len(clashes) != testCase.expectedClashes {
					t.Errorf("Expected %d clash points, got %d", testCase.expectedClashes, len(clashes))
				}

				if testCase.name == "Head-on collision" {
					// Verify collision point is (1,0)
					for point := range clashes {
						if point.P.X != 1 || point.P.Y != 0 {
							t.Errorf("Expected collision at (1,0), got (%f,%f)", point.P.X, point.P.Y)
						}
					}
				}
			},
		)
	}
}
