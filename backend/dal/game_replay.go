package dal

import (
	"islandBattles/backend/models"
	"islandBattles/lobby/domain"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// StoreGameReplayOnce locks the bound game and atomically saves every generation
// and its replay. Failed or canceled simulations leave no partial generations.
func StoreGameReplayOnce(database *gorm.DB, gameID uint, simulate func(*gorm.DB) ([]byte, error)) error {
	return database.Transaction(func(transaction *gorm.DB) error {
		var games []models.Game
		if err := transaction.Clauses(clause.Locking{Strength: "UPDATE"}).Where(&models.Game{Model: gorm.Model{ID: gameID}}).Limit(1).Find(&games).Error; err != nil {
			return err
		}
		if len(games) == 0 {
			return domain.ErrNotFound
		}
		if len(games[0].Replay) > 0 {
			return nil
		}
		replay, err := simulate(transaction)
		if err != nil {
			return err
		}
		return transaction.Model(&games[0]).Updates(&models.Game{Replay: replay}).Error
	})
}

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
