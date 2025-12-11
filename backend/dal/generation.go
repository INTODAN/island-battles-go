package dal

import (
	"islandBattles/backend/models"

	"gorm.io/gorm"
)

func CreateGeneration(db *gorm.DB, generation *models.Generation) (uint, error) {
	err := db.Create(&generation).Error
	if err != nil {
		return 0, err
	}
	return generation.ID, nil
}

func GetGeneration(db *gorm.DB, generationID uint) (*models.Generation, error) {
	var generation models.Generation
	err := db.Preload("Map").First(&generation, generationID).Error
	if err != nil {
		return nil, err
	}
	return &generation, nil
}
