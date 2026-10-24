package testutils

import (
	"islandBattles/backend/models"
	"testing"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// CreateSoldierGroupForTest places a group directly in its generation, as if it
// had been sent earlier. It takes the game's next persistent group ID and fills
// in the default clash radius and upgrade levels.
func CreateSoldierGroupForTest(t *testing.T, database *gorm.DB, soldierGroup *models.SoldierGroup) {
	t.Helper()
	if err := soldierGroup.Upgrades.Validate(); err != nil {
		t.Fatal(err)
	}
	soldierGroup.Upgrades = soldierGroup.Upgrades.Clone()
	soldierGroup.ClashRadius = soldierGroup.EffectiveClashRadius()
	err := database.Transaction(func(transaction *gorm.DB) error {
		var generations []models.Generation
		if err := transaction.Select("ID", "GameID").Where(&models.Generation{Model: gorm.Model{ID: soldierGroup.GenerationID}}).Limit(1).Find(&generations).Error; err != nil {
			return err
		}
		if len(generations) == 0 {
			return gorm.ErrRecordNotFound
		}
		var games []models.Game
		if err := transaction.Clauses(clause.Locking{Strength: "UPDATE"}).Select("ID", "NextSoldierGroupID").
			Where(&models.Game{Model: gorm.Model{ID: generations[0].GameID}}).Limit(1).Find(&games).Error; err != nil {
			return err
		}
		if len(games) == 0 {
			return gorm.ErrRecordNotFound
		}
		soldierGroup.PersistentSoldierGroupID = games[0].NextSoldierGroupID
		if err := transaction.Model(&games[0]).Select("NextSoldierGroupID").Updates(&models.Game{NextSoldierGroupID: games[0].NextSoldierGroupID + 1}).Error; err != nil {
			return err
		}
		return transaction.Create(soldierGroup).Error
	})
	if err != nil {
		t.Fatal(err)
	}
}
