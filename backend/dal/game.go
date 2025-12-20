package dal

import (
	"islandBattles/backend/models"

	"gorm.io/gorm"
)

// CreateGame creates a game in the db with no generations and no players (Empty lists)
func CreateGame(db *gorm.DB) (uint, error) {
	game := models.Game{
		Generations: []models.Generation{},
		Players:     []models.Player{},
	}

	result := db.Create(&game)
	if result.Error != nil {
		return 0, result.Error
	}

	return game.ID, nil
}

// AddPlayerToGame adds a player to the game with the gameId,
// assigning the next available TeamColor (highest + 1).
func AddPlayerToGame(db *gorm.DB, gameID uint) error {
	var lastPlayer models.Player
	// Find the player with the highest TeamColor for this game
	err := db.Where(&models.Player{GameID: gameID}).Order("team_color desc").First(&lastPlayer).Error

	newColor := 0
	if err == nil {
		// Player found, increment color
		newColor = lastPlayer.TeamColor + 1
	} else if err != gorm.ErrRecordNotFound {
		// Real error
		return err
	}

	player := models.Player{
		GameID:    gameID,
		TeamColor: newColor,
	}

	return db.Create(&player).Error
}
