package db_updates

import (
	"islandBattles/backend/dal"
	"islandBattles/backend/dal/commandHandlers/move_soldier_groups/internal"

	"gorm.io/gorm"
)

// Survivors complete their movement, so small sacrifices cannot pin an army.
func UpdateSoldierGroupPositions(database *gorm.DB, movements []internal.SoldierGroupMoveDelta) error {
	for _, movement := range movements {
		if movement.SoldierGroup.SoldierAmount == 0 {
			continue
		}
		if err := dal.UpdateSoldierGroupPosition(database, movement.SoldierGroup, movement.End); err != nil {
			return err
		}
	}
	return nil
}
