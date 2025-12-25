package services

import (
	"fmt"
	"islandBattles/backend/dal"
	"islandBattles/backend/dal/commandHandlers"
	"islandBattles/backend/models"

	"gorm.io/gorm"
)

func MigrateGeneration(db *gorm.DB, GameID uint, commands []models.CommandFields) (uint, error) {
	latestGeneration, err := dal.GetLatestGenerationForGame(db, GameID)
	if err != nil {
		return 0, err
	}
	// Get previous generation to get its map ID
	previousGeneration, err := dal.GetGeneration(db, latestGeneration.ID)
	if err != nil {
		return 0, err
	}

	// Duplicate the map with a deep copy
	newMapID, err := dal.DuplicateMap(db, previousGeneration.MapID)
	if err != nil {
		return 0, err
	}

	// Create the new generation linking to the new map
	migratedGeneration := models.Generation{
		GameID: previousGeneration.GameID,
		MapID:  newMapID,
	}
	migratedGenerationID, err := dal.CreateGeneration(db, &migratedGeneration)
	if err != nil {
		return 0, err
	}

	// TODO: Duplicate the soldier groups with a deep copy
	//newSoldierGroups =

	increaseSoldiersAmountCommand := models.CommandFields{
		CommandType: models.IncreaseSoldierAmount,
		Parameters:  []byte("{}")}

	commands = append(commands, increaseSoldiersAmountCommand)

	// Iterate through the provided commands and apply their changes
	for _, cmd := range commands {
		err := commandHandlers.ProcessCommand(db, &cmd, migratedGenerationID)
		if err != nil {
			fmt.Println("Error processing command:", err)
			return 0, err
		}
	}

	return migratedGenerationID, nil
}
