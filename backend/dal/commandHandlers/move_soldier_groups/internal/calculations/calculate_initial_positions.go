package calculations

import (
	"islandBattles/backend/dal/commandHandlers/move_soldier_groups/internal"
	"islandBattles/backend/models"
	"math"

	"github.com/jackc/pgx/v5/pgtype"
)

func CalculateInitialPositions(soldierGroups []models.SoldierGroup) []internal.SoldierGroupMoveDelta {
	var soldierGroupsPositions []internal.SoldierGroupMoveDelta

	for i := range soldierGroups {
		soldierGroup := &soldierGroups[i]
		newPosition := moveTowards(soldierGroup.Position, soldierGroup.DestinationCastle.Position)
		soldierGroupsPositions = append(
			soldierGroupsPositions, internal.SoldierGroupMoveDelta{
				Start:        soldierGroup.Position,
				End:          newPosition,
				SoldierGroup: soldierGroup,
			},
		)
	}
	return soldierGroupsPositions
}

// moveTowards creates a new position that is closer to the destination by models.SoldierGroupSpeedPerGeneration units
func moveTowards(currentPosition pgtype.Point, destination pgtype.Point) pgtype.Point {
	dx := destination.P.X - currentPosition.P.X
	dy := destination.P.Y - currentPosition.P.Y
	distance := math.Sqrt(dx*dx + dy*dy)

	if distance <= models.SoldierGroupSpeedPerGeneration {
		return destination
	}

	xRatio := dx / distance
	yRatio := dy / distance

	newX := currentPosition.P.X + xRatio*models.SoldierGroupSpeedPerGeneration
	newY := currentPosition.P.Y + yRatio*models.SoldierGroupSpeedPerGeneration

	return pgtype.Point{
		P:     pgtype.Vec2{X: newX, Y: newY},
		Valid: true,
	}
}
