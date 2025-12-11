package dal

import (
	"islandBattles/backend/models"

	"gorm.io/gorm"
)

func CreateMapTemplate(db *gorm.DB, mapTemplate *models.MapTemplate) (uint, error) {
	err := db.Create(&mapTemplate).Error
	if err != nil {
		return 0, err
	}
	return mapTemplate.ID, nil
}

func CreateMapFromTemplate(db *gorm.DB, templateID uint) (uint, error) {
	var template models.MapTemplate
	// Preload CastleTemplates so we can copy them
	if err := db.Preload("CastleTemplates").First(&template, templateID).Error; err != nil {
		return 0, err
	}

	newMap := models.Map{
		MapFields: template.MapFields,
	}

	var castles []models.Castle
	for _, ct := range template.CastleTemplates {
		castles = append(castles, models.Castle{
			CastleFields:  ct.CastleFields,
			SoldierAmount: template.NeutralStartingSoldierAmount,
		})
	}
	newMap.Castles = castles

	if err := db.Create(&newMap).Error; err != nil {
		return 0, err
	}

	return newMap.ID, nil
}

func DuplicateMap(db *gorm.DB, originalMapID uint) (uint, error) {
	var originalMap models.Map
	if err := db.Preload("Castles").First(&originalMap, originalMapID).Error; err != nil {
		return 0, err
	}

	newMap := models.Map{
		MapFields: originalMap.MapFields,
	}

	var newCastles []models.Castle
	for _, originalCastle := range originalMap.Castles {
		newCastles = append(newCastles, models.Castle{
			CastleFields:  originalCastle.CastleFields,
			SoldierAmount: originalCastle.SoldierAmount,
		})
	}
	newMap.Castles = newCastles

	if err := db.Create(&newMap).Error; err != nil {
		return 0, err
	}

	return newMap.ID, nil
}

func GetMap(db *gorm.DB, mapID uint) (*models.Map, error) {
	var m models.Map
	if err := db.Preload("Castles").First(&m, mapID).Error; err != nil {
		return nil, err
	}
	return &m, nil
}
