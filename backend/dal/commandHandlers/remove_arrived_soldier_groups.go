package commandHandlers

import (
	"islandBattles/backend/dal"
	"islandBattles/backend/models"
	"sort"

	"gorm.io/gorm"
)

type RemoveArrivedSoldierGroupsParams struct{}

// RemoveArrivedSoldierGroupsHandler Remove all soldier groups that arrived to their destination.
func RemoveArrivedSoldierGroupsHandler(
	db *gorm.DB,
	generation *models.Generation,
	parameters RemoveArrivedSoldierGroupsParams,
) error {
	// Use a stable snapshot of the arrivals, but read each destination from the
	// refreshed generation so an earlier arrival's damage/capture is preserved.
	soldierGroups := append([]models.SoldierGroup(nil), generation.SoldierGroups...)
	sort.SliceStable(soldierGroups, func(left, right int) bool {
		return soldierGroups[left].PersistentSoldierGroupID < soldierGroups[right].PersistentSoldierGroupID
	})
	for _, soldierGroup := range soldierGroups {
		if soldierGroup.Position == soldierGroup.DestinationCastle.Position {
			castle, err := dal.GetCastleFromGeneration(generation, soldierGroup.DestinationCastle.PersistentCastleID)
			if err != nil {
				return err
			}
			soldierGroup.DestinationCastle = *castle
			err = handleDestinationArrival(db, generation, &soldierGroup)
			if err != nil {
				return err
			}
		}
	}

	// Update the generation with the new list of soldier groups.
	return nil
}

func handleDestinationArrival(db *gorm.DB, generation *models.Generation, soldierGroup *models.SoldierGroup) error {
	soldierAmountDiff := calculateSoldierAmountDiff(soldierGroup)

	if soldierAmountDiff < 0 {
		soldierAmountDiff = -soldierAmountDiff
		err := dal.UpdateCastleTeamColor(db, &soldierGroup.DestinationCastle, soldierGroup.TeamColor)
		if err != nil {
			return err
		}
	}

	err := dal.UpdateCastleSoldierGroupAmount(
		db, &soldierGroup.DestinationCastle,
		uint(soldierAmountDiff),
	)
	if err != nil {
		return err
	}

	err = dal.DeleteSoldierGroup(db, soldierGroup.ID)
	if err != nil {
		return err
	}
	return dal.RefreshGeneration(db, generation)
}

func calculateSoldierAmountDiff(soldierGroup *models.SoldierGroup) int {
	soldierAmountToAdd := -int(soldierGroup.SoldierAmount)
	if soldierGroup.DestinationCastle.TeamColor == soldierGroup.TeamColor {
		soldierAmountToAdd = -soldierAmountToAdd
	}

	soldierAmountDiff := int(soldierGroup.DestinationCastle.SoldierAmount) + soldierAmountToAdd
	return soldierAmountDiff
}
