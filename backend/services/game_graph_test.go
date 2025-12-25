package services

import (
	"islandBattles/backend/dal"
	"islandBattles/backend/internal/testutils"
	"islandBattles/backend/models"
	"os"
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
		_, err := MigrateGeneration(db, generation.GameID, []models.CommandFields{})
		if err != nil {
			t.Fatal(err)
		}
	}

	htmlContent, err := GetGameGraph(db, generation.GameID)
	if err != nil {
		t.Fatal(err)
	}

	_, exists := os.LookupEnv("EXPORT_GAME_GRAPH")

	if exists {
		file, err := os.CreateTemp("", "game_graph_*.html")
		if err != nil {
			t.Fatalf("error creating temporary file: %s", err)
		}
		if _, err := file.WriteString(htmlContent); err != nil {
			t.Fatal(err)
		}
		err = file.Close()
		if err != nil {
			t.Fatalf("error closing temporary file: %s", err)
		}
		t.Logf("created temporary file: %s", file.Name())
	}
}
