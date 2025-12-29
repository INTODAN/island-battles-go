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
	err := db.Preload("Map.Castles").Preload("SoldierGroups").Preload("Commands").Preload("SoldierGroups.OriginCastle").Preload(
		"SoldierGroups.DestinationCastle").First(&generation,
		generationID).Error
	if err != nil {
		return nil, err
	}
	return &generation, nil
}

func GetLatestGenerationForGame(db *gorm.DB, gameID uint) (*models.Generation, error) {
	var generation models.Generation
	err := db.Preload("Map.Castles").
		Where(&models.Generation{GameID: gameID}).
		Order(clause.OrderByColumn{Column: clause.Column{Name: "id"}, Desc: true}).
		First(&generation).Error
	if err != nil {
		return nil, err
	}
	return &generation, nil
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
