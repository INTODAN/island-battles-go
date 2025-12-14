package commandHandlers

import (
	"fmt"
	"islandBattles/backend/dal"
	"islandBattles/backend/models"

	"gorm.io/gorm"
)

type CommandHandler[T any] func(db *gorm.DB, generation *models.Generation, parameters T) error
type CommandHandlerWrapper[T any] func(db *gorm.DB, generationID uint, parameters T) error

func WrapCommandHandler[T any](handler CommandHandler[T]) CommandHandlerWrapper[any] {
	return func(db *gorm.DB, generationID uint, rawParameters any) error {
		parsedParameters, ok := rawParameters.(T)
		if !ok {
			return fmt.Errorf("invalid param type: expected %T, got %T", *new(T), rawParameters)
		}
		generation, err := dal.GetGeneration(db, generationID)
		if err != nil {
			println("Error getting generation from database", err)
			return err
		}
		return handler(db, generation, parsedParameters)
	}
}

var Mapping = map[models.CommandType]CommandHandlerWrapper[any]{
	models.IncreaseSoldierAmount: WrapCommandHandler(IncreaseSoldierAmountHandler),
}
