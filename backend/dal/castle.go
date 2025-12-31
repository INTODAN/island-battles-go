package dal

import (
	"fmt"
	"islandBattles/backend/models"

	"gorm.io/gorm"
)

func GetCastleFromGeneration(generation *models.Generation, logicalCastleID uint) (*models.Castle, error) {
	for i := range generation.Map.Castles {
		if generation.Map.Castles[i].LogicalCastleID == logicalCastleID {
			return &generation.Map.Castles[i], nil
		}
	}
	return nil, fmt.Errorf("castle with LogicalCastleID %d not found in generation %d", logicalCastleID, generation.ID)
}

func UpdateCastleSoldierGroupAmount(db *gorm.DB, castle *models.Castle, soldierAmount uint) error {
	castle.SoldierAmount = soldierAmount
	return db.Save(castle).Error
}

func UpdateCastleTeamColor(db *gorm.DB, castle *models.Castle, teamColor uint) error {
	castle.TeamColor = teamColor
	return db.Save(castle).Error
}
