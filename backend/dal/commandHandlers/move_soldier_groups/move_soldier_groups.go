package move_soldier_groups

import (
	"islandBattles/backend/models"

	"github.com/jackc/pgx/v5/pgtype"
	"gorm.io/gorm"
)

type MoveSoldierGroupsParams struct{}
type soldierGroupMoveDelta struct {
	Start        pgtype.Point
	End          pgtype.Point
	SoldierGroup *models.SoldierGroup
}

func MoveSoldierGroupsHandler(db *gorm.DB, generation *models.Generation, parameters MoveSoldierGroupsParams) error {
	soldierGroupsPositions := calculateInitialPositions(generation.SoldierGroups)
	clashPositions, err := calculateClashPositions(soldierGroupsPositions)
	if err != nil {
		return err
	}

	err = updateSoldierGroupPositions(db, soldierGroupsPositions, clashPositions)
	if err != nil {
		return err
	}

	err = updateSoldierGroupAmounts(db, clashPositions)
	if err != nil {
		return err
	}

	return db.Model(generation).Association("SoldierGroups").Find(&generation.SoldierGroups)
}
