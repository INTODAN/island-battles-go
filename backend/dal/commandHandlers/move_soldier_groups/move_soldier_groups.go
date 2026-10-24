package move_soldier_groups

import (
	"islandBattles/backend/dal"
	"islandBattles/backend/dal/commandHandlers/move_soldier_groups/internal/calculations"
	"islandBattles/backend/dal/commandHandlers/move_soldier_groups/internal/db_updates"
	"islandBattles/backend/models"

	"gorm.io/gorm"
)

type MoveSoldierGroupsParams struct{}

func MoveSoldierGroupsHandler(db *gorm.DB, generation *models.Generation, parameters MoveSoldierGroupsParams) error {
	soldierGroupsPositions, err := calculations.CalculateInitialPositions(generation.SoldierGroups)
	if err != nil {
		return err
	}
	clashes, err := calculations.CalculateClashPositions(soldierGroupsPositions)
	if err != nil {
		return err
	}
	if err := dal.StoreGenerationClashes(db, generation, calculations.ClashEvents(clashes)); err != nil {
		return err
	}

	err = db_updates.UpdateSoldierGroupAmounts(db, clashes)
	if err != nil {
		return err
	}

	err = db_updates.UpdateSoldierGroupPositions(db, soldierGroupsPositions)
	if err != nil {
		return err
	}

	return db.Model(generation).Association("SoldierGroups").Find(&generation.SoldierGroups)
}
