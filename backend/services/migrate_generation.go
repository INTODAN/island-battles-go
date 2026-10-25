package services

import (
	"islandBattles/backend/dal"
	"islandBattles/backend/models"
	"islandBattles/backend/simulation"

	"gorm.io/gorm"
)

// MigrateGeneration plays one turn: it loads the latest generation once, applies
// the commands and the end-of-turn steps in memory, and saves the result as the
// next generation. A rejected command rolls back the whole turn.
func MigrateGeneration(database *gorm.DB, gameID uint, commands []models.CommandFields) (uint, error) {
	generationID, _, err := migrateGeneration(database, gameID, commands)
	return generationID, err
}

// migrateGeneration is MigrateGeneration, and also returns the new board so
// the turn can apply the end rule to it.
func migrateGeneration(database *gorm.DB, gameID uint, commands []models.CommandFields) (uint, simulation.GameState, error) {
	turnCommands := append(append([]models.CommandFields{}, commands...), simulation.EndOfTurnCommands()...)
	decodedCommands, err := simulation.DecodeCommands(turnCommands)
	if err != nil {
		return 0, simulation.GameState{}, err
	}
	var generationID uint
	var next simulation.GameState
	err = database.Transaction(func(transaction *gorm.DB) error {
		state, err := dal.LoadGameState(transaction, gameID)
		if err != nil {
			return err
		}
		var events []simulation.Event
		next, events, err = simulation.Simulate(state, decodedCommands)
		if err != nil {
			return err
		}
		generationID, err = dal.SaveGeneration(transaction, gameID, next, turnCommands, simulation.Clashes(events))
		return err
	})
	if err != nil {
		return 0, simulation.GameState{}, err
	}
	return generationID, next, nil
}
