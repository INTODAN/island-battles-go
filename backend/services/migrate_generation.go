package services

import (
	"fmt"
	"islandBattles/backend/dal"
	"islandBattles/backend/dal/commandHandlers"
	"islandBattles/backend/models"

	"gorm.io/gorm"
)

// MigrateGeneration commits the whole turn or rolls it back when a command is rejected.
func MigrateGeneration(database *gorm.DB, gameID uint, commands []models.CommandFields) (uint, error) {
	var generationID uint
	err := database.Transaction(func(transaction *gorm.DB) error {
		var err error
		generationID, err = migrateGeneration(transaction, gameID, commands)
		return err
	})
	if err != nil {
		return 0, err
	}
	return generationID, nil
}

func migrateGeneration(db *gorm.DB, GameID uint, commands []models.CommandFields) (uint, error) {
	previousGeneration, err := dal.GetLatestGenerationForGame(db, GameID)
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

	for _, soldierGroup := range previousGeneration.SoldierGroups {
		_, err := dal.DuplicateSoldierGroup(db, migratedGenerationID, soldierGroup.ID)
		if err != nil {
			return 0, err
		}
	}
	increaseSoldiersAmountCommand := models.CommandFields{
		CommandType: models.IncreaseSoldierAmount,
		Parameters:  []byte("{}")}

	moveSoldierGroupsCommand := models.CommandFields{
		CommandType: models.MoveSoldierGroups,
		Parameters:  []byte("{}"),
	}

	removeArrivedSoldierGroupsCommand := models.CommandFields{
		CommandType: models.RemoveArrivedSoldierGroups,
		Parameters:  []byte("{}"),
	}

	commands = append(commands, moveSoldierGroupsCommand)
	commands = append(commands, increaseSoldiersAmountCommand)
	commands = append(commands, removeArrivedSoldierGroupsCommand)

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
