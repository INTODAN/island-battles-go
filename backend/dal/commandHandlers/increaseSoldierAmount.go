package commandHandlers

import (
	"islandBattles/backend/dal"
	"islandBattles/backend/models"

	"gorm.io/gorm"
)

type IncreaseSoldierAmountParams struct{}

// Increase the soldier amount of each castle in the generation's map.
func IncreaseSoldierAmountHandler(db *gorm.DB, generation *models.Generation, parameters IncreaseSoldierAmountParams) error {
	const amount = 1 // TODO: implement upgrade system and get amount from castles.

	m, err := dal.GetMap(db, generation.MapID)
	if err != nil {
		return err
	}

	for i := range m.Castles {
		m.Castles[i].SoldierAmount += amount
	}

	if err := db.Save(&m.Castles).Error; err != nil {
		return err
	}
	return nil
}
