package move_soldier_groups

import (
	"math"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
)

func TestMoveTowards(t *testing.T) {
	startPosition := pgtype.Point{Valid: true, P: pgtype.Vec2{X: 0, Y: 0}}
	newDestination := pgtype.Point{Valid: true, P: pgtype.Vec2{X: 2, Y: 2}}
	anotherNewDestination := pgtype.Point{Valid: true, P: pgtype.Vec2{X: 10, Y: 10}}
	newPosition := moveTowards(startPosition, newDestination)
	anotherNewPosition := moveTowards(startPosition, anotherNewDestination)
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
