package move_soldier_groups

import (
	"islandBattles/backend/dal/commandHandlers/move_soldier_groups/internal/calculations"
	"islandBattles/backend/dal/commandHandlers/move_soldier_groups/internal/db_updates"
	"islandBattles/backend/models"

	"gorm.io/gorm"
)

type MoveSoldierGroupsParams struct{}

func MoveSoldierGroupsHandler(db *gorm.DB, generation *models.Generation, parameters MoveSoldierGroupsParams) error {
	soldierGroupsPositions := calculations.CalculateInitialPositions(generation.SoldierGroups)
	clashPositions, err := calculations.CalculateClashPositions(soldierGroupsPositions)
	if err != nil {
		return err
	}

	err = db_updates.UpdateSoldierGroupPositions(db, soldierGroupsPositions, clashPositions)
	if err != nil {
		return err
	}

	err = db_updates.UpdateSoldierGroupAmounts(db, clashPositions)
	if err != nil {
		return err
	}

	return db.Model(generation).Association("SoldierGroups").Find(&generation.SoldierGroups)
}
