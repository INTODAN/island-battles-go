package move_soldier_groups

import (
	"cmp"
	"islandBattles/backend/dal"
	"islandBattles/backend/models"
	"slices"

	"github.com/jackc/pgx/v5/pgtype"
	"gorm.io/gorm"
)

func updateSoldierGroupAmounts(
	db *gorm.DB,
	generation *models.Generation,
	clashPositions map[pgtype.Point][]uint,
) error {
	soldierGroupsMap := make(map[uint]*models.SoldierGroup)
	for i := range generation.SoldierGroups {
		soldierGroupsMap[generation.SoldierGroups[i].ID] = &generation.SoldierGroups[i]
	}
	for _, clashedSoldierGroupIDs := range clashPositions {
		slices.SortFunc(
			clashedSoldierGroupIDs, func(firstID, secondID uint) int {
				return cmp.Compare(soldierGroupsMap[firstID].SoldierAmount, soldierGroupsMap[secondID].SoldierAmount)
			},
		)

		soldierAmountsPerTeam := make(map[uint]uint)
		for _, id := range clashedSoldierGroupIDs {
			soldierAmountsPerTeam[soldierGroupsMap[id].TeamColor] += soldierGroupsMap[id].SoldierAmount
		}

		for _, id := range clashedSoldierGroupIDs {
			soldierGroup := soldierGroupsMap[id]
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

			if newSoldierAmount == 0 {
				if err := dal.DeleteSoldierGroup(db, soldierGroup.ID); err != nil {
					return err
				}
			} else {
				if err := dal.UpdateSoldierGroupAmount(db, soldierGroup, newSoldierAmount); err != nil {
					return err
				}
			}
		}

	}
	return nil
}
