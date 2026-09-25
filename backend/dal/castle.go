package dal

import (
	"fmt"
	"islandBattles/backend/models"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func GetCastleFromGeneration(generation *models.Generation, persistentCastleID uint) (*models.Castle, error) {
	for i := range generation.Map.Castles {
		if generation.Map.Castles[i].PersistentCastleID == persistentCastleID {
			return &generation.Map.Castles[i], nil
		}
	}
	return nil, fmt.Errorf("castle with PersistentCastleID %d not found in generation %d", persistentCastleID, generation.ID)
}

func UpdateCastleSoldierGroupAmount(db *gorm.DB, castle *models.Castle, soldierAmount uint) error {
	castle.SoldierAmount = soldierAmount
	return db.Save(castle).Error
}

func UpdateCastleTeamColor(db *gorm.DB, castle *models.Castle, teamColor uint) error {
	castle.TeamColor = teamColor
	return db.Save(castle).Error
}

// UpgradeCastle buys one level from the current persisted balance. The lock and
// single update keep the level and soldier deduction together, including when
// called outside a generation transaction or with a stale castle snapshot.
func UpgradeCastle(database *gorm.DB, castle *models.Castle, upgradeType models.UpgradeType) error {
	if castle.ID == 0 {
		return fmt.Errorf("castle must be persisted before upgrading")
	}
	var purchased models.Castle
	err := database.Transaction(func(transaction *gorm.DB) error {
		var castles []models.Castle
		if err := transaction.Clauses(clause.Locking{Strength: "UPDATE"}).Where(&models.Castle{Model: gorm.Model{ID: castle.ID}}).Limit(1).Find(&castles).Error; err != nil {
			return err
		}
		if len(castles) == 0 {
			return fmt.Errorf("castle %d not found", castle.ID)
		}
		purchased = castles[0]
		upgrades, err := purchased.Upgrades.Upgraded(upgradeType)
		if err != nil {
			return err
		}
		cost, err := upgrades.Cost(upgradeType)
		if err != nil {
			return err
		}
		if purchased.SoldierAmount < cost {
			return fmt.Errorf("%w: %s level %d costs %d, castle has %d", models.ErrInsufficientSoldiers, upgradeType, upgrades[upgradeType], cost, purchased.SoldierAmount)
		}
		purchased.Upgrades = upgrades
		purchased.SoldierAmount -= cost
		// Explicit selection includes a zero balance when the castle pays exactly.
		return transaction.Model(&purchased).Select("Upgrades", "SoldierAmount").Updates(&purchased).Error
	})
	if err != nil {
		return err
	}
	castle.Upgrades = purchased.Upgrades
	castle.SoldierAmount = purchased.SoldierAmount
	return nil
}
