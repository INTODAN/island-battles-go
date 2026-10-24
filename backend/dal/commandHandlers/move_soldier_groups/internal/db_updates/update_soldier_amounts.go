package db_updates

import (
	"islandBattles/backend/dal"
	"islandBattles/backend/dal/commandHandlers/move_soldier_groups/internal"

	"gorm.io/gorm"
)

func UpdateSoldierGroupAmounts(database *gorm.DB, clashes []internal.Clash) error {
	for _, clash := range clashes {
		for _, participant := range clash.Participants {
			soldierGroup := participant.SoldierGroup
			if participant.SoldiersAfter == 0 {
				if err := dal.DeleteSoldierGroup(database, soldierGroup.ID); err != nil {
					return err
				}
				soldierGroup.SoldierAmount = 0
			} else if err := dal.UpdateSoldierGroupAmount(database, soldierGroup, participant.SoldiersAfter); err != nil {
				return err
			}
		}
	}
	return nil
}
