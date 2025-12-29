package commandHandlers

import (
	"fmt"
	"islandBattles/backend/dal"
	"islandBattles/backend/models"

	"gorm.io/gorm"
)

type SendSoldierGroupParams struct {
	models.SoldierGroupFields
}

func SendSoldierGroupHandler(db *gorm.DB, generation *models.Generation, parameters SendSoldierGroupParams) error {
	originCastle, err := dal.GetCastleFromGeneration(generation, parameters.OriginCastleID)
	if err != nil {
		return err
	}

	destinationCastle, err := dal.GetCastleFromGeneration(generation, parameters.DestinationCastleID)
	if err != nil {
		return err
	}

	parameters.OriginCastleID = originCastle.ID
	parameters.DestinationCastleID = destinationCastle.ID

	if originCastle.SoldierAmount < parameters.SoldierAmount {
		return fmt.Errorf("castle doesn't have enough soldier amount")
	}
	err = dal.UpdateSoldierGroupAmount(db, originCastle, originCastle.SoldierAmount-parameters.SoldierAmount)
	if err != nil {
		return err
	}
	dbSoldierGroup := models.SoldierGroup{SoldierGroupFields: parameters.SoldierGroupFields, GenerationID: generation.ID,
		Position: originCastle.Position}
	_, err = dal.CreateSoldierGroup(db, &dbSoldierGroup)
	if err != nil {
		return err
	}
	return nil
}
