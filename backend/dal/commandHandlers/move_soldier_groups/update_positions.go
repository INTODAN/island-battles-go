package move_soldier_groups

import (
	"islandBattles/backend/dal"
	"islandBattles/backend/models"

	"gorm.io/gorm"
)

func updateSoldierGroupPositions(
	db *gorm.DB,
	generation *models.Generation,
	soldierGroupsPositions map[uint]SoldierGroupMoveDelta,
) error {
	for i := range generation.SoldierGroups {
		soldierGroup := &generation.SoldierGroups[i]
		err := dal.UpdateSoldierGroupPosition(db, soldierGroup, soldierGroupsPositions[soldierGroup.ID].End)
		if err != nil {
			return err
		}
	}
	return nil
}
