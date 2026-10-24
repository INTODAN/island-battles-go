package dal

import (
	"fmt"
	"islandBattles/backend/models"

	"gorm.io/gorm"
)

func GetCastleFromGeneration(generation *models.Generation, persistentCastleID uint) (*models.Castle, error) {
	for i := range generation.Map.Castles {
		if generation.Map.Castles[i].PersistentCastleID == persistentCastleID {
			return &generation.Map.Castles[i], nil
		}
	}
	return nil, fmt.Errorf("castle with PersistentCastleID %d not found in generation %d", persistentCastleID, generation.ID)
}

func UpdateCastleSoldierGroupAmount(db *gorm.DB, castle *models.Castle, soldierAmount uint) error {
	castle.SoldierAmount = soldierAmount
	return db.Save(castle).Error
}
