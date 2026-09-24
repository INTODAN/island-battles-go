package handlers_test

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"islandBattles/backend/dal"
	"islandBattles/backend/handlers"
	"islandBattles/backend/internal/testutils"
	"islandBattles/backend/models"
	"islandBattles/backend/routes"

	"github.com/danielgtaylor/huma/v2/humatest"
)

func TestUpgradeCastleHTTP(t *testing.T) {
	database := testutils.InitTestDB(t)
	generationID := testutils.CreateGenerationForTest(t, database)
	generation, err := dal.GetGeneration(database, generationID)
	if err != nil {
		t.Fatal(err)
	}
	_, api := humatest.New(t)
	routes.SetupRoutes(api, &handlers.Env{DB: database})
	castle, err := dal.GetCastleFromGeneration(generation, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := dal.UpdateCastleSoldierGroupAmount(database, castle, 9); err != nil {
		t.Fatal(err)
	}
	body := fmt.Sprintf(`{"game_id":%d,"commands":[{"CommandType":"upgrade_castle","Parameters":{"logical_castle_id":0,"upgrade_type":"speed"}}]}`, generation.GameID)
	response := api.Post("/generation/migrate", "Content-Type: application/json", strings.NewReader(body))
	if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), "not enough soldiers") {
		t.Fatalf("unexpected affordability response: %d %s", response.Code, response.Body.String())
	}
	if err := dal.UpdateCastleSoldierGroupAmount(database, castle, 60); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		upgradeType string
		status      int
		level       int
		soldiers    uint
	}{
		{"unknown", http.StatusUnprocessableEntity, 1, 60},
		{"speed", http.StatusOK, 2, 51},
		{"speed", http.StatusOK, 3, 32},
		{"speed", http.StatusOK, 4, 3},
		{"speed", http.StatusConflict, 4, 3},
	} {
		body := fmt.Sprintf(`{"game_id":%d,"commands":[{"CommandType":"upgrade_castle","Parameters":{"logical_castle_id":0,"upgrade_type":%q}}]}`, generation.GameID, test.upgradeType)
		response := api.Post("/generation/migrate", "Content-Type: application/json", strings.NewReader(body))
		if response.Code != test.status {
			t.Fatalf("status=%d want=%d body=%s", response.Code, test.status, response.Body.String())
		}
		latest, err := dal.GetLatestGenerationForGame(database, generation.GameID)
		if err != nil {
			t.Fatal(err)
		}
		castle, err := dal.GetCastleFromGeneration(latest, 0)
		if err != nil {
			t.Fatal(err)
		}
		if castle.Upgrades[models.SpeedUpgrade] != test.level {
			t.Fatalf("castle level=%d want=%d", castle.Upgrades[models.SpeedUpgrade], test.level)
		}
		if castle.SoldierAmount != test.soldiers {
			t.Fatalf("castle soldiers=%d want=%d", castle.SoldierAmount, test.soldiers)
		}
	}
}
