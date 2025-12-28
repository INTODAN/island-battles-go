package dal

import (
	"islandBattles/backend/models"

	"gorm.io/gorm"
)

func GetCastle(db *gorm.DB, castleID uint) (*models.Castle, error) {
	var castle models.Castle
	err := db.Preload("Generations.Map.Castles").First(&castle, castleID).Error
	if err != nil {
		return nil, err
	}
	return &castle, nil
}

func UpdateSoldierGroupAmount(db *gorm.DB, castle *models.Castle, soldierAmount uint) error {
	castle.SoldierAmount = soldierAmount
	return db.Save(castle).Error
}
