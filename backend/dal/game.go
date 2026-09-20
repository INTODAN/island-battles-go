package dal

import (
	"islandBattles/backend/models"

	"gorm.io/gorm"
)

// GetGameByID retrieves a game by its ID, preloading all generations.
// It returns the game if found, nil if not found, and an error if the query fails.
func GetGameByID(db *gorm.DB, gameID uint) (*models.Game, error) {
	var games []models.Game
	err := db.Preload("Generations", WithFullGeneration).Limit(1).Find(&games, gameID).Error
	if err != nil {
		return nil, err
	}
	if len(games) == 0 {
		return nil, nil
	}
	return &games[0], nil
}

// CreateGame creates a detached game fixture with no generations or lobby state.
// It returns the created game's ID and an error if the creation fails.
func CreateGame(db *gorm.DB) (uint, error) {
	game := models.Game{Generations: []models.Generation{}}

	result := db.Create(&game)
	if result.Error != nil {
		return 0, result.Error
	}

	return game.ID, nil
}
