package calculations

import (
	"islandBattles/backend/dal/commandHandlers/move_soldier_groups/internal"
	"islandBattles/backend/models"
	"math"

	"github.com/jackc/pgx/v5/pgtype"
)

func CalculateInitialPositions(soldierGroups []models.SoldierGroup) ([]internal.SoldierGroupMoveDelta, error) {
	var soldierGroupsPositions []internal.SoldierGroupMoveDelta

	for i := range soldierGroups {
		soldierGroup := &soldierGroups[i]
		speed, err := soldierGroup.Upgrades.Value(models.SpeedUpgrade)
		if err != nil {
			return nil, err
		}
		newPosition := moveTowards(soldierGroup.Position, soldierGroup.DestinationCastle.Position, speed)
		soldierGroupsPositions = append(
			soldierGroupsPositions, internal.SoldierGroupMoveDelta{
				Start:        soldierGroup.Position,
				End:          newPosition,
				SoldierGroup: soldierGroup,
			},
		)
	}
	return soldierGroupsPositions, nil
}

// moveTowards advances by this group’s snapshotted speed without overshooting.
func moveTowards(currentPosition pgtype.Point, destination pgtype.Point, speed float64) pgtype.Point {
	dx := destination.P.X - currentPosition.P.X
	dy := destination.P.Y - currentPosition.P.Y
	distance := math.Sqrt(dx*dx + dy*dy)

	if distance <= speed {
		return destination
	}

	xRatio := dx / distance
	yRatio := dy / distance

	newX := currentPosition.P.X + xRatio*speed
	newY := currentPosition.P.Y + yRatio*speed

	return pgtype.Point{
		P:     pgtype.Vec2{X: newX, Y: newY},
		Valid: true,
	}
}
