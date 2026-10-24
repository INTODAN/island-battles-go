package dal

import (
	"errors"
	"fmt"
	"islandBattles/backend/models"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ErrMapTemplateNameTaken rejects a template whose map_name another template uses.
var ErrMapTemplateNameTaken = errors.New("map template name is already taken")

// CreateMapTemplate validates a map file and stores it as a default map. An
// invalid template returns a *models.MapTemplateError and stores nothing.
func CreateMapTemplate(db *gorm.DB, template models.MapTemplateView) (uint, error) {
	if err := models.ValidateMapTemplate(template); err != nil {
		return 0, err
	}
	var count int64
	if err := db.Model(&models.MapTemplate{}).Where(&models.MapTemplate{MapFields: models.MapFields{MapName: template.MapName}}).Count(&count).Error; err != nil {
		return 0, err
	}
	if count > 0 {
		return 0, fmt.Errorf("%w: %q", ErrMapTemplateNameTaken, template.MapName)
	}
	stored := models.NewMapTemplate(template)
	if err := db.Create(&stored).Error; err != nil {
		return 0, err
	}
	return stored.ID, nil
}

// CreateMapFromTemplate stores a new game map built from a stored template.
// Stored templates are not validated again, so templates saved before a
// validation rule existed keep working.
func CreateMapFromTemplate(db *gorm.DB, templateID uint) (uint, error) {
	var template models.MapTemplate
	if err := db.Preload(clause.Associations).First(&template, templateID).Error; err != nil {
		return 0, err
	}

	newMap := models.BuildMap(template.View())
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
			CastleFields:  originalCastle.Clone(),
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
