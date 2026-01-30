package dal

import (
	"islandBattles/backend/models"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// WithFullGeneration is a scope that preloads all associations of a Generation
func WithFullGeneration(db *gorm.DB) *gorm.DB {
	return db.Preload("Map.Castles").
		Preload("SoldierGroups.OriginCastle").
		Preload("SoldierGroups.DestinationCastle").
		Preload("Commands")
}

func RefreshGeneration(db *gorm.DB, generation *models.Generation) error {
	return db.Scopes(WithFullGeneration).First(&generation, generation.ID).Error
}

func CreateGeneration(db *gorm.DB, generation *models.Generation) (uint, error) {
	err := db.Create(&generation).Error
	if err != nil {
		return 0, err
	}
	return generation.ID, nil
}

func GetGeneration(db *gorm.DB, generationID uint) (*models.Generation, error) {
	var generation models.Generation
	err := db.Scopes(WithFullGeneration).First(&generation, generationID).Error
	if err != nil {
		return nil, err
	}
	return &generation, nil
}

func GetLatestGenerationForGame(db *gorm.DB, gameID uint) (*models.Generation, error) {
	var generation models.Generation
	err := db.Scopes(WithFullGeneration).
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
