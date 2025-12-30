package commandHandlers

import (
	"islandBattles/backend/dal"
	"islandBattles/backend/models"

	"gorm.io/gorm"
)

type RemoveArrivedSoldierGroupsParams struct{}

// RemoveArrivedSoldierGroupsHandler Remove all soldier groups that arrived to their destination.
func RemoveArrivedSoldierGroupsHandler(db *gorm.DB, generation *models.Generation,
	parameters RemoveArrivedSoldierGroupsParams) error {

	for _, soldierGroup := range generation.SoldierGroups {
		destinationCastle, err := dal.GetCastleFromGeneration(generation, soldierGroup.DestinationCastle.LogicalCastleID)
		if err != nil {
			return err
		}
		if soldierGroup.Position == destinationCastle.Position {
			soldierAmountToAdd := -int(soldierGroup.SoldierAmount)
			if destinationCastle.TeamColor == soldierGroup.TeamColor {
				soldierAmountToAdd = -soldierAmountToAdd
			}

			soldierAmountDiff := int(destinationCastle.SoldierAmount) + soldierAmountToAdd
			if soldierAmountDiff < 0 {
				soldierAmountDiff = -soldierAmountDiff
				err := dal.UpdateCastleTeamColor(db, destinationCastle, soldierGroup.TeamColor)
				if err != nil {
					return err
				}
			}

			err := dal.UpdateSoldierGroupAmount(db, destinationCastle,
				uint(soldierAmountDiff))
			if err != nil {
				return err
			}

			err = dal.DeleteSoldierGroup(db, soldierGroup.ID)
			if err != nil {
				return err
			}
		}
	}

	// Update the generation with the new list of soldier groups.
	return db.Model(generation).Association("SoldierGroups").Find(&generation.SoldierGroups)
}
