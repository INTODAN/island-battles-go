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
	err := db.Preload("Map.Castles").Preload("SoldierGroups").First(&generation, generationID).Error
	if err != nil {
		return nil, err
	}
	return &generation, nil
}

func GetLatestGenerationForGame(db *gorm.DB, gameID uint) (*models.Generation, error) {
	var generation models.Generation
	err := db.Preload("Map.Castles").Where("game_id = ?", gameID).Order("created_at desc").First(&generation).Error
	if err != nil {
		return nil, err
	}
	return &generation, nil
}

func GetGenerationsForGame(db *gorm.DB, gameID uint) ([]models.Generation, error) {
	var generations []models.Generation
	err := db.Preload("Map.Castles").Where("game_id = ?", gameID).Order("created_at asc").Find(&generations).Error
	if err != nil {
		return nil, err
	}
	return generations, nil
}

func CreateGenerationFromTemplate(db *gorm.DB, mapTemplateID uint, gameID uint) (uint, error) {
	mapID, err := CreateMapFromTemplate(db, mapTemplateID)
	if err != nil {
		return 0, err
	}

	generation := models.Generation{
		MapID:  mapID,
		GameID: gameID,
	}

	generationID, err := CreateGeneration(db, &generation)
	if err != nil {
		return 0, err
	}
	return generationID, nil
}
