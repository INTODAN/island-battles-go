package dal

import (
	"islandBattles/backend/models"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
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
	err := db.Preload(clause.Associations).First(&generation, generationID).Error
	if err != nil {
		return nil, err
	}
	return &generation, nil
}

func CreateGenerationFromTemplate(db *gorm.DB, mapTemplateID uint) (uint, error) {
	mapID, err := CreateMapFromTemplate(db, mapTemplateID)
	if err != nil {
		return 0, err
	}

	generation := models.Generation{
		MapID: mapID,
	}

	generationID, err := CreateGeneration(db, &generation)
	if err != nil {
		return 0, err
	}
	return generationID, nil
}
