package move_soldier_groups

import (
	"islandBattles/backend/models"

	"github.com/jackc/pgx/v5/pgtype"
	"gorm.io/gorm"
)

type MoveSoldierGroupsParams struct{}
type SoldierGroupMoveDelta struct {
	Start        pgtype.Point
	End          pgtype.Point
	SoldierGroup *models.SoldierGroup
}

func MoveSoldierGroupsHandler(db *gorm.DB, generation *models.Generation, parameters MoveSoldierGroupsParams) error {
	soldierGroupsPositions := calculateInitialPositions(generation.SoldierGroups)
	clashPositions := calculateClashPositions(soldierGroupsPositions)

	err := updateSoldierGroupPositions(db, generation, soldierGroupsPositions)
	if err != nil {
		return err
	}

	err = updateSoldierGroupAmounts(db, generation, clashPositions)
	if err != nil {
		return err
	}

	return db.Model(generation).Association("SoldierGroups").Find(&generation.SoldierGroups)
}
