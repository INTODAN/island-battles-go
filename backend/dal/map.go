package dal

import (
	"fmt"
	"islandBattles/backend/models"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func CreateMapTemplate(db *gorm.DB, mapTemplate *models.MapTemplate) (uint, error) {
	// Check if a map template with the same name already exists
	var count int64
	db.Where(&models.MapTemplate{MapFields: models.MapFields{MapName: mapTemplate.MapName}}).Count(&count)

	if count > 0 {
		return 0, fmt.Errorf("map template with name '%s' already exists", mapTemplate.MapName)
	}

	for i := range mapTemplate.CastleTemplates {
		mapTemplate.CastleTemplates[i].CastleID = uint(i)
	}

	err := db.Create(&mapTemplate).Error
	if err != nil {
		return 0, err
	}
	return mapTemplate.ID, nil
}

func CreateMapFromTemplate(db *gorm.DB, templateID uint) (uint, error) {
	var template models.MapTemplate
	if err := db.Preload(clause.Associations).First(&template, templateID).Error; err != nil {
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
	if err := db.Preload(clause.Associations).First(&originalMap, originalMapID).Error; err != nil {
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
	if err := db.Preload(clause.Associations).First(&m, mapID).Error; err != nil {
		return nil, err
	}
	return &m, nil
}
