package commandHandlers

import (
	"fmt"
	"islandBattles/backend/dal"
	"islandBattles/backend/models"

	"gorm.io/gorm"
)

type SendSoldierGroupParams struct {
	OriginLogicalCastleID      uint
	DestinationLogicalCastleID uint
	SoldierAmount              uint
}

func SendSoldierGroupHandler(db *gorm.DB, generation *models.Generation, parameters SendSoldierGroupParams) error {
	originCastle, err := dal.GetCastleFromGeneration(generation, parameters.OriginLogicalCastleID)
	if err != nil {
		return err
	}

	destinationCastle, err := dal.GetCastleFromGeneration(generation, parameters.DestinationLogicalCastleID)
	if err != nil {
		return err
	}

	soldierGroupFields := models.SoldierGroupFields{
		SoldierAmount:       parameters.SoldierAmount,
		OriginCastleID:      originCastle.ID,
		DestinationCastleID: destinationCastle.ID,
	}

	if originCastle.SoldierAmount < parameters.SoldierAmount {
		return fmt.Errorf("castle doesn't have enough soldier amount")
	}
	err = dal.UpdateCastleSoldierGroupAmount(db, originCastle, originCastle.SoldierAmount-parameters.SoldierAmount)
	if err != nil {
		return err
	}
	dbSoldierGroup := models.SoldierGroup{
		SoldierGroupFields: soldierGroupFields, GenerationID: generation.ID,
		Position: originCastle.Position, TeamColor: originCastle.TeamColor,
		Upgrades: originCastle.Upgrades.Clone(),
	}
	_, err = dal.CreateSoldierGroup(db, &dbSoldierGroup)
	if err != nil {
		return err
	}
	return nil
}
