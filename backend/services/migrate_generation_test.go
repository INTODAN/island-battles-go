package services

import (
	"islandBattles/backend/dal"
	"islandBattles/backend/internal/testutils"
	"islandBattles/backend/models"
	"testing"
)

func TestMigrateGeneration(t *testing.T) {
	db := testutils.InitTestDB(t)
	generationID := testutils.CreateGenerationForTest(t, db)
	generation, err := dal.GetGeneration(db, generationID)
	if err != nil {
		t.Fatal(err)
	}
	migratedGenerationID, err := MigrateGeneration(db, generation.GameID, []models.CommandFields{})
	if err != nil {
		t.Fatal(err)
	}
	if generationID >= migratedGenerationID {
		t.Fatal("generation should be migrated")
	}
	migratedGeneration, err := dal.GetGeneration(db, migratedGenerationID)
	if err != nil {
		t.Fatal(err)
	}
	if migratedGeneration.Map.Castles == nil {
		t.Fatal("castles should not be nil, preloading failed.")
	}
	if migratedGeneration.Map.Castles[0].SoldierAmount <= migratedGeneration.Map.NeutralStartingSoldierAmount {
		t.Fatal("Soldier amount did not increase as expected")
	}
	if len(migratedGeneration.Commands) < 1 {
		t.Fatal("expected at least one command")
	}
}
