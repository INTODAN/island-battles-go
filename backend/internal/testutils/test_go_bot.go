package testutils

import (
	"testing"

	"islandBattles/backend/models"

	"gorm.io/gorm"
)

// AssignGoBotForTest has the backend play a roster player of a started game
// with the default bot of a level, from the next game request on, as if the
// player had chosen it before the start.
func AssignGoBotForTest(t *testing.T, database *gorm.DB, gameID uint, playerID string, level uint) {
	t.Helper()
	result := database.Model(&models.FrozenGamePlayer{}).Where(&models.FrozenGamePlayer{GameID: gameID, PlayerID: playerID}).
		Select("BotLevel").Updates(&models.FrozenGamePlayer{BotLevel: &level})
	if result.Error != nil {
		t.Fatal(result.Error)
	}
	if result.RowsAffected != 1 {
		t.Fatalf("player %s is not on game %d's roster", playerID, gameID)
	}
}
