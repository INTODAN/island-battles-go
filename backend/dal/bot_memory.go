package dal

import (
	"islandBattles/backend/models"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// FindBotMemory returns the memory a player sent with their latest completed
// turn of a game, or nil when they have none.
func FindBotMemory(database *gorm.DB, gameID uint, playerID string) (models.BotMemory, error) {
	// GORM drops zero-valued struct conditions, which would match any player.
	if gameID == 0 || playerID == "" {
		return nil, nil
	}
	var memories []models.GamePlayerBotMemory
	if err := database.Select("Memory").Where(&models.GamePlayerBotMemory{GameID: gameID, PlayerID: playerID}).Limit(1).Find(&memories).Error; err != nil {
		return nil, err
	}
	if len(memories) == 0 {
		return nil, nil
	}
	return memories[0].Memory, nil
}

// LoadBotMemories returns every stored memory of a game by player ID.
// Players without memory are absent.
func LoadBotMemories(database *gorm.DB, gameID uint) (map[string]models.BotMemory, error) {
	if gameID == 0 {
		return nil, nil
	}
	var memories []models.GamePlayerBotMemory
	if err := database.Select("PlayerID", "Memory").Where(&models.GamePlayerBotMemory{GameID: gameID}).Find(&memories).Error; err != nil {
		return nil, err
	}
	byPlayer := make(map[string]models.BotMemory, len(memories))
	for _, memory := range memories {
		byPlayer[memory.PlayerID] = memory.Memory
	}
	return byPlayer, nil
}

// SaveBotMemory replaces a player's memory for a game, or removes it when the
// record has none.
func SaveBotMemory(database *gorm.DB, record *models.GamePlayerBotMemory) error {
	if len(record.Memory) == 0 {
		if record.GameID == 0 || record.PlayerID == "" {
			return nil
		}
		return database.Where(&models.GamePlayerBotMemory{GameID: record.GameID, PlayerID: record.PlayerID}).Delete(&models.GamePlayerBotMemory{}).Error
	}
	return database.Clauses(clause.OnConflict{UpdateAll: true}).Create(record).Error
}
