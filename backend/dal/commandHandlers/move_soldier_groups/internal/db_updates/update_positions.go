package db_updates

import (
	"islandBattles/backend/dal"
	"islandBattles/backend/dal/commandHandlers/move_soldier_groups/internal"
	"islandBattles/backend/models"

	"github.com/jackc/pgx/v5/pgtype"
	"gorm.io/gorm"
)

func UpdateSoldierGroupPositions(
	db *gorm.DB,
	soldierGroupsPositions []internal.SoldierGroupMoveDelta,
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
	soldierGroupsPositions []internal.SoldierGroupMoveDelta,
	clashPositions map[pgtype.Point][]*models.SoldierGroup,
) {
	soldierGroupsPositionsMap := make(map[uint]*internal.SoldierGroupMoveDelta)
	for i := range soldierGroupsPositions {
		soldierGroupsPositionsMap[soldierGroupsPositions[i].SoldierGroup.ID] = &soldierGroupsPositions[i]
	}

	for clashedPosition, clashedSoldierGroups := range clashPositions {
		for _, soldierGroup := range clashedSoldierGroups {
			soldierGroupsPositionsMap[soldierGroup.ID].End = clashedPosition
		}
	}
}
