package move_soldier_groups

import (
	"islandBattles/backend/dal"
	"islandBattles/backend/models"

	"github.com/jackc/pgx/v5/pgtype"
	"gorm.io/gorm"
)

func updateSoldierGroupPositions(
	db *gorm.DB,
	soldierGroupsPositions []soldierGroupMoveDelta,
	clashPositions map[pgtype.Point][]*models.SoldierGroup,
) error {
	updateInitialSoldierGroupsPositionsWithClashPositions(soldierGroupsPositions, clashPositions)
	for i := range soldierGroupsPositions {
		err := dal.UpdateSoldierGroupPosition(
			db, soldierGroupsPositions[i].SoldierGroup,
			soldierGroupsPositions[i].End,
		)
		if err != nil {
			return err
		}
	}
	return nil
}

func updateInitialSoldierGroupsPositionsWithClashPositions(
	soldierGroupsPositions []soldierGroupMoveDelta,
	clashPositions map[pgtype.Point][]*models.SoldierGroup,
) {
	for clashedPosition, clashedSoldierGroups := range clashPositions {
		for _, soldierGroup := range clashedSoldierGroups {
			for i := range soldierGroupsPositions {
				if soldierGroupsPositions[i].SoldierGroup.ID == soldierGroup.ID {
					vector := soldierGroupsPositions[i]
					vector.End = clashedPosition
					soldierGroupsPositions[i] = vector
				}
			}
		}
	}
}
