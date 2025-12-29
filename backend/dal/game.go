package dal

import (
	"errors"
	"islandBattles/backend/models"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func GetGame(db *gorm.DB, gameID uint) (*models.Game, error) {
	var game models.Game
	err := db.Preload("Generations", WithFullGeneration).First(&game, gameID).Error
	if err != nil {
		return nil, err
	}
	return &game, nil
}

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

// AddPlayerToGame adds a player to the game with the gameID,
// assigning the next available TeamColor (highest + 1).
func AddPlayerToGame(db *gorm.DB, gameID uint) error {
	var highestTeamColorPlayer models.Player
	// Find the player with the highest TeamColor for this game
	err := db.Where(&models.Player{GameID: gameID}).Order(clause.OrderByColumn{Column: clause.Column{Name: "team_color"},
		Desc: false,
	}).First(
		&highestTeamColorPlayer).
		Error

	newColor := 0
	if err == nil {
		// Player found, increment color
		newColor = highestTeamColorPlayer.TeamColor + 1
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		// Real error
		return err
	}

	player := models.Player{
		GameID:    gameID,
		TeamColor: newColor,
	}

	return db.Create(&player).Error
}
