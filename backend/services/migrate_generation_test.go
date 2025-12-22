package services

import (
	"islandBattles/backend/internal/testutils"
	"islandBattles/backend/models"
	"testing"
)

func TestMigrateGeneration(t *testing.T) {
	db := testutils.InitTestDB(t)
	generationID := testutils.CreateGenerationForTest(t, db)
	migratedGenerationID, err := MigrateGeneration(db, generationID, []models.CommandFields{})
	if err != nil {
		t.Fatal(err)
	}
	if generationID >= migratedGenerationID {
		t.Fatal("generation should be migrated")
	}
}
