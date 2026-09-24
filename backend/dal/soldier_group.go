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

func UpdateSoldierGroupAmount(db *gorm.DB, soldierGroup *models.SoldierGroup, newSoldierAmount uint) error {
	soldierGroup.SoldierAmount = newSoldierAmount
	return db.Save(soldierGroup).Error
}

func CreateSoldierGroup(db *gorm.DB, soldierGroup *models.SoldierGroup) (uint, error) {
	if err := soldierGroup.Upgrades.Validate(); err != nil {
		return 0, err
	}
	soldierGroup.Upgrades = soldierGroup.Upgrades.Clone()
	var lastGroups []models.SoldierGroup
	err := db.Where(&models.SoldierGroup{GenerationID: soldierGroup.GenerationID}).
		Order(clause.OrderByColumn{Column: clause.Column{Name: "logical_soldier_group_id"}, Desc: true}).
		Limit(1).
		Find(&lastGroups).Error

	if err != nil {
		return 0, err
	}

	if len(lastGroups) > 0 {
		soldierGroup.LogicalSoldierGroupID = lastGroups[0].LogicalSoldierGroupID + 1
	} else {
		soldierGroup.LogicalSoldierGroupID = 0
	}

	err = db.Create(&soldierGroup).Error

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
		Position:              originalSoldierGroup.Position,
		GenerationID:          migratedGenerationID,
		LogicalSoldierGroupID: originalSoldierGroup.LogicalSoldierGroupID,
		TeamColor:             originalSoldierGroup.TeamColor,
		ClashRadius:           originalSoldierGroup.ClashRadius,
		Upgrades:              originalSoldierGroup.Upgrades.Clone(),
	}

	if err := db.Create(&newSoldierGroup).Error; err != nil {
		return 0, err
	}

	return newSoldierGroup.ID, nil
}

func DeleteSoldierGroup(db *gorm.DB, soldierGroupID uint) error {
	return db.Delete(&models.SoldierGroup{}, soldierGroupID).Error
}
