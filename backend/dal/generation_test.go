package dal_test

import (
	"islandBattles/backend/dal"
	"islandBattles/backend/internal/testutils"
	"islandBattles/backend/models"
	"islandBattles/backend/services"
	"testing"
)

func TestGetGeneration(t *testing.T) {
	db := testutils.InitTestDB(t)
	_, err := dal.GetGeneration(db, testutils.CreateGenerationForTest(t, db))
	if err != nil {
		t.Fatal(err)
	}
}

func TestGetLatestGeneration(t *testing.T) {
	db := testutils.InitTestDB(t)

	generationID := testutils.CreateGenerationForTest(t, db)
	generation, err := dal.GetGeneration(db, generationID)
	if err != nil {
		t.Fatal(err)
	}
	latestGeneration, err := dal.GetLatestGenerationForGame(db, generation.GameID)
	if err != nil {
		t.Fatal(err)
	}
	if latestGeneration.ID != generationID {
		t.Fatal("Latest generation ID not correct")
	}
	newGenerationID, err := services.MigrateGeneration(db, generation.GameID, []models.CommandFields{})
	if err != nil {
		t.Fatal(err)
	}
	latestGeneration, err = dal.GetLatestGenerationForGame(db, generation.GameID)
	if err != nil {
		t.Fatal(err)
	}
	if latestGeneration.ID != newGenerationID {
		t.Fatal("Latest generation ID not correct")
	}
}
