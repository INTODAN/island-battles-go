package commandHandlers

import (
	"fmt"
	"islandBattles/backend/models"

	"gorm.io/gorm"
)

type CommandHandler[T any] func(db *gorm.DB, generation *models.Generation, parameters T) error

func WrapCommandHandler[T any](handler CommandHandler[T]) CommandHandler[any] {
	return func(db *gorm.DB, generation *models.Generation, rawParameters any) error {
		parsedParameters, ok := rawParameters.(T)
		if !ok {
			return fmt.Errorf("invalid param type: expected %T, got %T", *new(T), rawParameters)
		}
		return handler(db, generation, parsedParameters)
	}
}

var CommandHandlersMapping = map[models.CommandType]CommandHandler[any]{
	models.IncreaseSoldierAmount: WrapCommandHandler(IncreaseSoldierAmountHandler),
}
