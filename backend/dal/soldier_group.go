package dal

import (
	"fmt"
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

// CreateSoldierGroup assigns the game's next persistent ID. IDs stay unique for
// the whole game, including after their groups are defeated or arrive.
func CreateSoldierGroup(db *gorm.DB, soldierGroup *models.SoldierGroup) (uint, error) {
	if err := soldierGroup.Upgrades.Validate(); err != nil {
		return 0, err
	}
	soldierGroup.Upgrades = soldierGroup.Upgrades.Clone()
	soldierGroup.ClashRadius = soldierGroup.EffectiveClashRadius()
	err := db.Transaction(func(transaction *gorm.DB) error {
		persistentID, err := reserveSoldierGroupID(transaction, soldierGroup.GenerationID)
		if err != nil {
			return err
		}
		soldierGroup.PersistentSoldierGroupID = persistentID
		return transaction.Create(soldierGroup).Error
	})
	if err != nil {
		return 0, err
	}
	return soldierGroup.ID, nil
}

// reserveSoldierGroupID locks the generation's game so concurrent spawns cannot
// receive the same ID.
func reserveSoldierGroupID(transaction *gorm.DB, generationID uint) (uint, error) {
	if generationID == 0 {
		return 0, fmt.Errorf("soldier group must belong to a persisted generation")
	}
	var generations []models.Generation
	if err := transaction.Select("ID", "GameID").Where(&models.Generation{Model: gorm.Model{ID: generationID}}).Limit(1).Find(&generations).Error; err != nil {
		return 0, err
	}
	if len(generations) == 0 || generations[0].GameID == 0 {
		return 0, fmt.Errorf("generation %d does not belong to a game", generationID)
	}
	var games []models.Game
	if err := transaction.Clauses(clause.Locking{Strength: "UPDATE"}).Select("ID", "NextSoldierGroupID").
		Where(&models.Game{Model: gorm.Model{ID: generations[0].GameID}}).Limit(1).Find(&games).Error; err != nil {
		return 0, err
	}
	if len(games) == 0 {
		return 0, fmt.Errorf("game %d not found", generations[0].GameID)
	}
	persistentID := games[0].NextSoldierGroupID
	if err := transaction.Model(&games[0]).Update("NextSoldierGroupID", persistentID+1).Error; err != nil {
		return 0, err
	}
	return persistentID, nil
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

	newOriginCastle, err := GetCastleFromGeneration(migratedGeneration, originalSoldierGroup.OriginCastle.PersistentCastleID)
	if err != nil {
		return 0, err
	}
	newDestinationCastle, err := GetCastleFromGeneration(migratedGeneration, originalSoldierGroup.DestinationCastle.PersistentCastleID)
	if err != nil {
		return 0, err
	}

	newSoldierGroup := models.SoldierGroup{
		SoldierGroupFields: models.SoldierGroupFields{
			SoldierAmount:       originalSoldierGroup.SoldierAmount,
			OriginCastleID:      newOriginCastle.ID,
			DestinationCastleID: newDestinationCastle.ID,
		},
		Position:                 originalSoldierGroup.Position,
		GenerationID:             migratedGenerationID,
		PersistentSoldierGroupID: originalSoldierGroup.PersistentSoldierGroupID,
		TeamColor:                originalSoldierGroup.TeamColor,
		ClashRadius:              originalSoldierGroup.ClashRadius,
		MovementPauseGenerations: originalSoldierGroup.MovementPauseGenerations,
		Upgrades:                 originalSoldierGroup.Upgrades.Clone(),
	}

	if err := db.Create(&newSoldierGroup).Error; err != nil {
		return 0, err
	}

	return newSoldierGroup.ID, nil
}

func DeleteSoldierGroup(db *gorm.DB, soldierGroupID uint) error {
	return db.Delete(&models.SoldierGroup{}, soldierGroupID).Error
}
