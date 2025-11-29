package dal

import (
	"islandBattles/backend/models"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func CreateMapTemplate(db *gorm.DB, input *models.MapTemplate) (uint, error) {
	err := db.Create(&input).Error
	if err != nil {
		return 0, err
	}
	return input.ID, nil
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
