package commandHandlers

import (
	"encoding/json"
	"fmt"
	"islandBattles/backend/dal"
	"islandBattles/backend/dal/commandHandlers/move_soldier_groups"
	"islandBattles/backend/models"

	"gorm.io/datatypes"
	"gorm.io/gorm"
)

type CommandHandler[T any] func(db *gorm.DB, generation *models.Generation, parameters T) error
type CommandHandlerWrapper func(db *gorm.DB, generationID uint, parameters datatypes.JSON) error

func WrapCommandHandler[TParams any](handler CommandHandler[TParams]) CommandHandlerWrapper {
	return func(db *gorm.DB, generationID uint, rawParameters datatypes.JSON) error {
		var parsedParameters TParams
		err := json.Unmarshal(rawParameters, &parsedParameters)
		if err != nil {
			return err
		}
		generation, err := dal.GetGeneration(db, generationID)
		if err != nil {
			return err
		}
		return handler(db, generation, parsedParameters)
	}
}

var Mapping = map[models.CommandType]CommandHandlerWrapper{
	models.IncreaseSoldierAmount: WrapCommandHandler[IncreaseSoldierAmountParams](IncreaseSoldierAmountHandler),
	models.MoveSoldierGroups: WrapCommandHandler[move_soldier_groups.MoveSoldierGroupsParams](
		move_soldier_groups.MoveSoldierGroupsHandler,
	),
	models.SendSoldierGroup:           WrapCommandHandler[SendSoldierGroupParams](SendSoldierGroupHandler),
	models.RemoveArrivedSoldierGroups: WrapCommandHandler[RemoveArrivedSoldierGroupsParams](RemoveArrivedSoldierGroupsHandler),
}

func ProcessCommand(db *gorm.DB, command *models.CommandFields, generationID uint) error {
	dbCommand := models.Command{CommandFields: *command, GenerationID: generationID}
	_, err := dal.CreateCommand(db, &dbCommand)
	if err != nil {
		return err
	}

	err = Mapping[dbCommand.CommandType](db, dbCommand.GenerationID, dbCommand.Parameters)
	if err != nil {
		fmt.Println("Error while handling command in dal", err)
		return err
	}

	return nil
}
