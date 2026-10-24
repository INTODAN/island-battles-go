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
	turnCommands := append(append([]models.CommandFields{}, commands...), simulation.EndOfTurnCommands()...)
	decodedCommands, err := simulation.DecodeCommands(turnCommands)
	if err != nil {
		return 0, err
	}
	var generationID uint
	err = database.Transaction(func(transaction *gorm.DB) error {
		state, err := dal.LoadGameState(transaction, gameID)
		if err != nil {
			return err
		}
		next, events, err := simulation.Simulate(state, decodedCommands)
		if err != nil {
			return err
		}
		generationID, err = dal.SaveGeneration(transaction, gameID, next, turnCommands, simulation.Clashes(events))
		return err
	})
	if err != nil {
		return 0, err
	}
	return generationID, nil
}
