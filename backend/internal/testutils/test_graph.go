package testutils

import (
	"islandBattles/backend/services"
	"os"
	"testing"

	"gorm.io/gorm"
)

func CreateGameGraphForTest(t *testing.T, db *gorm.DB, GameID uint) string {
	htmlContent, err := services.GetGameGraph(db, GameID)
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
	return htmlContent
}
