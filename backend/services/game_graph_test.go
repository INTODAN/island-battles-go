package services_test

import (
	"islandBattles/backend/dal"
	"islandBattles/backend/internal/testutils"
	"islandBattles/backend/models"
	"islandBattles/backend/services"
	"testing"
)

func TestGetGameGraph(t *testing.T) {
	db := testutils.InitTestDB(t)
	generationID := testutils.CreateGenerationForTest(t, db)
	generation, err := dal.GetGeneration(db, generationID)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 300-1; i++ {
		_, err := services.MigrateGeneration(db, generation.GameID, []models.CommandFields{})
		if err != nil {
			t.Fatal(err)
		}
	}
	_ = testutils.CreateGameGraphForTest(t, db, generation.GameID)
}

//func TestGameGraphWithSoldierGroups(t *testing.T) {
//	db := testutils.InitTestDB(t)
//}
