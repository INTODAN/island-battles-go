package dal

import (
	"islandBattles/backend/models"

	"gorm.io/gorm"
)

func CreateCommand(db *gorm.DB, command *models.Command) (uint, error) {
	err := db.Create(&command).Error
	if err != nil {
		return 0, err
	}
	return command.ID, nil
}
