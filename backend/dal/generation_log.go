package dal

import (
	"islandBattles/backend/models"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// CountGenerationLogs counts the game log lines a player has stored for a
// generation.
func CountGenerationLogs(database *gorm.DB, generationID uint, playerID string) (int64, error) {
	var count int64
	err := database.Model(&models.GenerationLog{}).Where(&models.GenerationLog{GenerationID: generationID, PlayerID: playerID}).Count(&count).Error
	return count, err
}

// StoreGenerationLogs appends lines in one statement. The caller numbers
// their sequences.
func StoreGenerationLogs(database *gorm.DB, lines []models.GenerationLog) error {
	if len(lines) == 0 {
		return nil
	}
	return database.Create(&lines).Error
}

// LoadGenerationLogs returns every stored line of a generation, ordered by
// player and then by sequence.
func LoadGenerationLogs(database *gorm.DB, generationID uint) ([]models.GenerationLog, error) {
	// GORM drops zero-valued struct conditions, which would match every generation.
	if generationID == 0 {
		return nil, nil
	}
	var lines []models.GenerationLog
	err := database.Where(&models.GenerationLog{GenerationID: generationID}).Order(clause.OrderBy{Columns: []clause.OrderByColumn{
		{Column: clause.Column{Name: "player_id"}}, {Column: clause.Column{Name: "sequence"}},
	}}).Find(&lines).Error
	return lines, err
}

// GameHasGeneration reports whether generationID is one of the game's
// generations.
func GameHasGeneration(database *gorm.DB, gameID, generationID uint) (bool, error) {
	if gameID == 0 || generationID == 0 {
		return false, nil
	}
	var generations []models.Generation
	err := database.Select("ID").Where(&models.Generation{Model: gorm.Model{ID: generationID}, GameID: gameID}).Limit(1).Find(&generations).Error
	return len(generations) > 0, err
}
