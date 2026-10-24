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
//
// map_templates has no unique index on map_name, so the name check and the
// insert run in one transaction that first takes an advisory lock on the name.
// Concurrent creates with the same name then run one after the other, and only
// the first one stores a template.
func CreateMapTemplate(db *gorm.DB, template models.MapTemplateView) (uint, error) {
	if err := models.ValidateMapTemplate(template); err != nil {
		return 0, err
	}
	stored := models.NewMapTemplate(template)
	err := db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("SELECT pg_advisory_xact_lock(hashtextextended(?, 0))", mapTemplateNameLockPrefix+template.MapName).Error; err != nil {
			return err
		}
		var count int64
		if err := tx.Model(&models.MapTemplate{}).Where(&models.MapTemplate{MapFields: models.MapFields{MapName: template.MapName}}).Count(&count).Error; err != nil {
			return err
		}
		if count > 0 {
			return fmt.Errorf("%w: %q", ErrMapTemplateNameTaken, template.MapName)
		}
		return tx.Create(&stored).Error
	})
	if err != nil {
		return 0, err
	}
	return stored.ID, nil
}

// mapTemplateNameLockPrefix namespaces the advisory lock key that
// CreateMapTemplate hashes from a map name.
const mapTemplateNameLockPrefix = "map_templates.map_name:"

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

func GetMap(db *gorm.DB, mapID uint) (*models.Map, error) {
	var m models.Map
	if err := db.Preload(clause.Associations).First(&m, mapID).Error; err != nil {
		return nil, err
	}
	return &m, nil
}
