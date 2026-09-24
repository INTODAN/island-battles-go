package dal

import (
	"islandBattles/backend/models"

	"gorm.io/gorm"
)

// GetLobbyGameReplay returns nil while the start transaction has not completed.
func GetLobbyGameReplay(database *gorm.DB, lobbyID string) ([]byte, error) {
	var games []models.Game
	if err := database.Select("replay").Where(&models.Game{LobbyID: &lobbyID}).Limit(1).Find(&games).Error; err != nil {
		return nil, err
	}
	if len(games) == 0 {
		return nil, nil
	}
	return games[0].Replay, nil
}
