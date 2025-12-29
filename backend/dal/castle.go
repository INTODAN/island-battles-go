package dal

import (
	"fmt"
	"islandBattles/backend/models"

	"gorm.io/gorm"
)

func GetCastleFromGeneration(generation *models.Generation, castleID uint) (*models.Castle, error) {
	for i := range generation.Map.Castles {
		if generation.Map.Castles[i].CastleID == castleID {
			return &generation.Map.Castles[i], nil
		}
	}
	return nil, fmt.Errorf("castle with CastleID %d not found in generation %d", castleID, generation.ID)
}

func UpdateSoldierGroupAmount(db *gorm.DB, castle *models.Castle, soldierAmount uint) error {
	castle.SoldierAmount = soldierAmount
	return db.Save(castle).Error
}
