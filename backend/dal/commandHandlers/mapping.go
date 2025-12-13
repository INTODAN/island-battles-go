package commandHandlers

import (
	"islandBattles/backend/models"

	"gorm.io/gorm"
)

type CommandHandler func(db *gorm.DB, generation *models.Generation, parameters any) error

var CommandHandlersMapping = map[models.CommandType]CommandHandler{
	models.IncreaseSoldierAmount: IncreaseSoldierAmountHandler,
}
