package db_updates

import (
	"islandBattles/backend/dal"
	"islandBattles/backend/dal/commandHandlers/move_soldier_groups/internal"
	"islandBattles/backend/models"

	"gorm.io/gorm"
)

func UpdateSoldierGroupAmounts(
	db *gorm.DB,
	clashes []internal.Clash,
) error {
	for _, clash := range clashes {
		clashedSoldierGroups := make([]*models.SoldierGroup, 0, len(clash.Participants))
		for _, participant := range clash.Participants {
			clashedSoldierGroups = append(clashedSoldierGroups, participant.SoldierGroup)
		}
		soldierAmountsPerTeam := getSoldierAmountsPerTeam(clashedSoldierGroups)

		for _, soldierGroup := range clashedSoldierGroups {
			newSoldierAmount := calculateNewSoldierAmount(soldierGroup, soldierAmountsPerTeam)
			err := updateSoldierGroupAmount(db, soldierGroup, newSoldierAmount)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func getSoldierAmountsPerTeam(clashedSoldierGroups []*models.SoldierGroup) map[uint]uint {
	soldierAmountsPerTeam := make(map[uint]uint)
	for _, soldierGroup := range clashedSoldierGroups {
		soldierAmountsPerTeam[soldierGroup.TeamColor] += soldierGroup.SoldierAmount
	}
	return soldierAmountsPerTeam
}

func calculateNewSoldierAmount(soldierGroup *models.SoldierGroup, soldierAmountsPerTeam map[uint]uint) uint {
	newSoldierAmount := soldierGroup.SoldierAmount
	for teamColor, amount := range soldierAmountsPerTeam {
		if teamColor == soldierGroup.TeamColor || amount == 0 {
			continue
		}
		enemyAmount := soldierAmountsPerTeam[teamColor]
		deduction := min(newSoldierAmount, enemyAmount)

		newSoldierAmount -= deduction
		soldierAmountsPerTeam[teamColor] -= deduction

		if newSoldierAmount == 0 {
			break
		}
	}
	return newSoldierAmount
}

func updateSoldierGroupAmount(db *gorm.DB, soldierGroup *models.SoldierGroup, newSoldierAmount uint) error {
	if newSoldierAmount == 0 {
		if err := dal.DeleteSoldierGroup(db, soldierGroup.ID); err != nil {
			return err
		}
	} else {
		if err := dal.UpdateSoldierGroupAmount(db, soldierGroup, newSoldierAmount); err != nil {
			return err
		}
	}
	return nil
}
