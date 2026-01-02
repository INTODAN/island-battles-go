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
	soldierGroupsPositionsMap := make(map[uint]*soldierGroupMoveDelta)
	for i := range soldierGroupsPositions {
		soldierGroupsPositionsMap[soldierGroupsPositions[i].SoldierGroup.ID] = &soldierGroupsPositions[i]
	}

	for clashedPosition, clashedSoldierGroups := range clashPositions {
		for _, soldierGroup := range clashedSoldierGroups {
			soldierGroupsPositionsMap[soldierGroup.ID].End = clashedPosition
		}
	}
}
