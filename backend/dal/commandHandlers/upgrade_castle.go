package commandHandlers

import (
	"islandBattles/backend/dal"
	"islandBattles/backend/models"

	"gorm.io/gorm"
)

type UpgradeCastleParams struct {
	LogicalCastleID uint               `json:"logical_castle_id"`
	UpgradeType     models.UpgradeType `json:"upgrade_type"`
}

func UpgradeCastleHandler(database *gorm.DB, generation *models.Generation, parameters UpgradeCastleParams) error {
	castle, err := dal.GetCastleFromGeneration(generation, parameters.LogicalCastleID)
	if err != nil {
		return err
	}
	return dal.UpgradeCastle(database, castle, parameters.UpgradeType)
}
