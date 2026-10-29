package handlers_test

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/humatest"
	"gorm.io/gorm"

	"islandBattles/backend/dal"
	"islandBattles/backend/handlers"
	"islandBattles/backend/internal/testutils"
	"islandBattles/backend/models"
)

// A missing game, and a game that retention has retired, have no graph.
func TestGameGraphOfAMissingOrRetiredGameIsNotFound(t *testing.T) {
	database := testutils.InitTestDB(t)
	generation, err := dal.GetGeneration(database, testutils.CreateGenerationForTest(t, database))
	if err != nil {
		t.Fatal(err)
	}
	_, api := humatest.New(t)
	huma.Register(api, huma.Operation{OperationID: "graph", Method: http.MethodGet, Path: "/game/{id}/graph"}, (&handlers.Env{DB: database}).GetGameGraph)
	graphPath := fmt.Sprintf("/game/%d/graph", generation.GameID)
	if response := api.Get(graphPath); response.Code != http.StatusOK {
		t.Fatalf("graph=%d %s", response.Code, response.Body.String())
	}
	if response := api.Get(fmt.Sprintf("/game/%d/graph", generation.GameID+1000)); response.Code != http.StatusNotFound {
		t.Fatalf("missing game graph=%d %s", response.Code, response.Body.String())
	}
	// Retiring soft-deletes the game row.
	if err := database.Delete(&models.Game{Model: gorm.Model{ID: generation.GameID}}).Error; err != nil {
		t.Fatal(err)
	}
	if response := api.Get(graphPath); response.Code != http.StatusNotFound {
		t.Fatalf("retired game graph=%d %s", response.Code, response.Body.String())
	}
}
