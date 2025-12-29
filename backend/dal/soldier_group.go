package dal

import (
	"islandBattles/backend/models"

	"github.com/jackc/pgx/v5/pgtype"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// UpdateSoldierGroupPosition updates the given soldier group's position to the received position in the db.
func UpdateSoldierGroupPosition(db *gorm.DB, soldierGroup *models.SoldierGroup, newPosition pgtype.Point) error {
	soldierGroup.Position = newPosition
	return db.Save(soldierGroup).Error
}

func CreateSoldierGroup(db *gorm.DB, soldierGroup *models.SoldierGroup) (uint, error) {
	err := db.Create(&soldierGroup).Error

	if err != nil {
		return 0, err
	}
	return soldierGroup.ID, nil
}

func DuplicateSoldierGroup(db *gorm.DB, migratedGenerationID uint, originSoldierGroupID uint) (uint, error) {
	var originalSoldierGroup models.SoldierGroup
	if err := db.Preload(clause.Associations).First(&originalSoldierGroup, originSoldierGroupID).Error; err != nil {
		return 0, err
	}

	migratedGeneration, err := GetGeneration(db, migratedGenerationID)
	if err != nil {
		return 0, err
	}

	newOriginCastle, err := GetCastleFromGeneration(migratedGeneration, originalSoldierGroup.OriginCastle.LogicalCastleID)
	if err != nil {
		return 0, err
	}
	newDestinationCastle, err := GetCastleFromGeneration(migratedGeneration, originalSoldierGroup.DestinationCastle.LogicalCastleID)
	if err != nil {
		return 0, err
	}

	newSoldierGroup := models.SoldierGroup{
		SoldierGroupFields: models.SoldierGroupFields{
			SoldierAmount:       originalSoldierGroup.SoldierAmount,
			OriginCastleID:      newOriginCastle.ID,
			DestinationCastleID: newDestinationCastle.ID,
		},
		Position:     originalSoldierGroup.Position,
		GenerationID: migratedGenerationID,
	}

	if err := db.Create(&newSoldierGroup).Error; err != nil {
		return 0, err
	}

	return newSoldierGroup.ID, nil
}
