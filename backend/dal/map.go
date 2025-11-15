package dal

import (
	"islandBattles/backend/models"

	"gorm.io/gorm"
)

func CreateMapTemplate(db *gorm.DB, input *models.MapTemplate) (uint, error) {
	err := db.Create(&input).Error
	if err != nil {
		return 0, err
	}
	return input.ID, nil
}
