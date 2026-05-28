package dal

import (
	"islandBattles/backend/models"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

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

func GetGameByRoomName(db *gorm.DB, roomName string) (*models.Game, error) {
	var games []models.Game
	err := db.Preload("Generations", WithFullGeneration).
		Where(&models.Game{RoomName: roomName}).
		Limit(1).
		Find(&games).Error
	if err != nil {
		return nil, err
	}
	if len(games) == 0 {
		return nil, nil
	}
	return &games[0], nil
}

// CreateGame creates a game in the db with no generations and no players (Empty lists)
func CreateGame(db *gorm.DB) (uint, error) {
	game := models.Game{
		Generations: []models.Generation{},
		Players:     []models.Player{},
		RoomName:    "test-game",
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
	var players []models.Player
	// Find the player with the highest TeamColor for this game
	err := db.Where(&models.Player{GameID: gameID}).
		Order(clause.OrderByColumn{Column: clause.Column{Name: "team_color"}, Desc: true}).
		Limit(1).
		Find(&players).Error

	if err != nil {
		return err
	}

	newColor := 1
	if len(players) > 0 {
		// Player found, increment color
		newColor = players[0].TeamColor + 1
	}

	player := models.Player{
		GameID:    gameID,
		TeamColor: newColor,
	}

	return db.Create(&player).Error
}
