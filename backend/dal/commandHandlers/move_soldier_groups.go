package commandHandlers

import (
	"islandBattles/backend/dal"
	"islandBattles/backend/models"
	"math"

	"github.com/jackc/pgx/v5/pgtype"
	"gorm.io/gorm"
)

type MoveSoldierGroupsParams struct{}

func MoveSoldierGroupsHandler(db *gorm.DB, generation *models.Generation, parameters MoveSoldierGroupsParams) error {
	for i := range generation.SoldierGroups {
		soldierGroup := &generation.SoldierGroups[i]
		newPosition := MoveTowards(soldierGroup.Position, soldierGroup.DestinationCastle.Position)
		err := dal.UpdateSoldierGroupPosition(db, soldierGroup, newPosition)
		if err != nil {
			return err
		}
	}
	return nil
}

// MoveTowards creates a new position that is closer to the destination by models.SoldierGroupSpeedPerGeneration points
func MoveTowards(currentPosition pgtype.Point, destination pgtype.Point) pgtype.Point {
	dx := destination.P.X - currentPosition.P.X
	dy := destination.P.Y - currentPosition.P.Y
	distance := math.Sqrt(dx*dx + dy*dy)

	if distance <= models.SoldierGroupSpeedPerGeneration {
		return destination
	}

	ratio := models.SoldierGroupSpeedPerGeneration / distance
	newX := currentPosition.P.X + dx*ratio
	newY := currentPosition.P.Y + dy*ratio

	return pgtype.Point{
		P:     pgtype.Vec2{X: newX, Y: newY},
		Valid: true,
	}
}
