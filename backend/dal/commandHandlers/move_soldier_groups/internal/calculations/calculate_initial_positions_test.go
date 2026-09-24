package calculations

import (
	"islandBattles/backend/models"
	"math"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"gorm.io/gorm"
)

func TestMoveTowards(t *testing.T) {
	startPosition := pgtype.Point{Valid: true, P: pgtype.Vec2{X: 0, Y: 0}}
	newDestination := pgtype.Point{Valid: true, P: pgtype.Vec2{X: 2, Y: 2}}
	anotherNewDestination := pgtype.Point{Valid: true, P: pgtype.Vec2{X: 10, Y: 10}}
	newPosition := moveTowards(startPosition, newDestination, 0.5)
	anotherNewPosition := moveTowards(startPosition, anotherNewDestination, 0.5)
	dx := newPosition.P.X - startPosition.P.X
	dy := newPosition.P.Y - startPosition.P.Y
	distance := math.Sqrt(dx*dx + dy*dy)

	dx = anotherNewPosition.P.X - startPosition.P.X
	dy = anotherNewPosition.P.Y - startPosition.P.Y
	anotherDistance := math.Sqrt(dx*dx + dy*dy)

	if distance != anotherDistance {
		t.Fatal("Distances should be equal regardless of destination distance: ", distance, anotherDistance)
	}
}

func TestCalculateInitialPositions(t *testing.T) {
	tests := []struct {
		name             string
		soldierGroups    []models.SoldierGroup
		expectedCount    int
		expectedDistance float64
	}{
		{
			name:             "Empty list",
			soldierGroups:    []models.SoldierGroup{},
			expectedCount:    0,
			expectedDistance: 0,
		},
		{
			name: "Single soldier group moving diagonally",
			soldierGroups: []models.SoldierGroup{
				{
					Model: gorm.Model{ID: 1},
					Position: pgtype.Point{
						P:     pgtype.Vec2{X: 0, Y: 0},
						Valid: true,
					},
					DestinationCastle: models.Castle{
						CastleFields: models.CastleFields{
							Position: pgtype.Point{
								P:     pgtype.Vec2{X: 10, Y: 10},
								Valid: true,
							},
						},
					},
				},
			},
			expectedCount:    1,
			expectedDistance: 0.5,
		},
		{
			name: "Single soldier group about to reach destination",
			soldierGroups: []models.SoldierGroup{
				{
					Model: gorm.Model{ID: 1},
					Position: pgtype.Point{
						P:     pgtype.Vec2{X: 9.7, Y: 0},
						Valid: true,
					},
					DestinationCastle: models.Castle{
						CastleFields: models.CastleFields{
							Position: pgtype.Point{
								P:     pgtype.Vec2{X: 10, Y: 0},
								Valid: true,
							},
						},
					},
				},
			},
			expectedCount:    1,
			expectedDistance: 0.3,
		},
	}

	for _, testCase := range tests {
		t.Run(
			testCase.name, func(t *testing.T) {
				result, err := CalculateInitialPositions(testCase.soldierGroups)
				if err != nil {
					t.Fatal(err)
				}

				if len(result) != testCase.expectedCount {
					t.Fatalf("Expected %d results, got %d", testCase.expectedCount, len(result))
				}

				if testCase.expectedCount > 0 {
					sg := testCase.soldierGroups[0]
					res := result[0]

					if res.SoldierGroup != &testCase.soldierGroups[0] {
						t.Errorf("Expected SoldierGroup pointer to match")
					}

					if res.Start != sg.Position {
						t.Errorf("Expected Start position to match input position")
					}

					// Check distance moved equals speed
					dx := res.End.P.X - res.Start.P.X
					dy := res.End.P.Y - res.Start.P.Y
					dist := math.Sqrt(dx*dx + dy*dy)

					// Allow for small float point errors
					if math.Abs(dist-testCase.expectedDistance) > 1e-9 {
						t.Errorf("Expected distance moved to be %f, got %f", 0.5, dist)
					}
				}
			},
		)
	}
}

func TestMovementUsesGroupUpgradeSnapshot(t *testing.T) {
	for level, speed := range []float64{.5, 1, 1.5, 2} {
		group := models.SoldierGroup{
			Upgrades:          models.Upgrades{models.SpeedUpgrade: level + 1},
			Position:          pgtype.Point{P: pgtype.Vec2{X: 0, Y: 0}, Valid: true},
			DestinationCastle: models.Castle{CastleFields: models.CastleFields{Position: pgtype.Point{P: pgtype.Vec2{X: 3, Y: 4}, Valid: true}}},
		}
		movements, err := CalculateInitialPositions([]models.SoldierGroup{group})
		if err != nil {
			t.Fatal(err)
		}
		position := movements[0].End.P
		if math.Abs(math.Hypot(position.X, position.Y)-speed) > 1e-9 {
			t.Fatalf("level %d moved %v", level+1, position)
		}
		group.DestinationCastle.Position.P = pgtype.Vec2{X: .1, Y: .1}
		movements, err = CalculateInitialPositions([]models.SoldierGroup{group})
		if err != nil || movements[0].End != group.DestinationCastle.Position {
			t.Fatal("overshot destination")
		}
	}
}
