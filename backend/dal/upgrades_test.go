package dal_test

import (
	"islandBattles/backend/dal"
	"islandBattles/backend/dal/commandHandlers"
	"islandBattles/backend/internal/testutils"
	"islandBattles/backend/models"
	"testing"
)

func TestUpgradeDefaultsMigrateExistingState(t *testing.T) {
	database := testutils.InitTestDB(t)
	generationID := testutils.CreateGenerationForTest(t, database)
	generation, err := dal.GetGeneration(database, generationID)
	if err != nil {
		t.Fatal(err)
	}
	if err := commandHandlers.SendSoldierGroupHandler(database, generation, commandHandlers.SendSoldierGroupParams{OriginLogicalCastleID: 0, DestinationLogicalCastleID: 1, SoldierAmount: 2}); err != nil {
		t.Fatal(err)
	}
	for _, model := range []any{&models.CastleTemplate{}, &models.Castle{}, &models.SoldierGroup{}} {
		if err := database.Migrator().DropColumn(model, "Upgrades"); err != nil {
			t.Fatal(err)
		}
	}
	if err := database.AutoMigrate(&models.CastleTemplate{}, &models.Castle{}, &models.SoldierGroup{}); err != nil {
		t.Fatal(err)
	}
	// Select only the new column to avoid a stale SELECT * prepared statement
	// after intentionally altering these tables on the same test connection.
	for _, model := range []any{&models.CastleTemplate{}, &models.Castle{}, &models.SoldierGroup{}} {
		var rows []struct {
			Upgrades models.Upgrades `gorm:"serializer:json"`
		}
		if err := database.Model(model).Select("upgrades").Find(&rows).Error; err != nil {
			t.Fatal(err)
		}
		if len(rows) == 0 {
			t.Fatal("fixture had no existing rows")
		}
		for _, row := range rows {
			if row.Upgrades[models.SpeedUpgrade] != 1 {
				t.Fatalf("missing migrated default: %+v", row)
			}
		}
	}
}
