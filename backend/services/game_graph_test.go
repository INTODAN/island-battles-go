package services_test

import (
	"encoding/json"
	"islandBattles/backend/dal"
	"islandBattles/backend/internal/testutils"
	"islandBattles/backend/models"
	"islandBattles/backend/services"
	"islandBattles/backend/simulation"
	"strings"
	"testing"
)

// A Lambda bundle holds only the binary, so the graph page must not read its
// controls or script from the working directory.
func TestGameGraphNeedsNoSourceTree(t *testing.T) {
	db := testutils.InitTestDB(t)
	generationID := testutils.CreateGenerationForTest(t, db)
	generation, err := dal.GetGeneration(db, generationID)
	if err != nil {
		t.Fatal(err)
	}
	t.Chdir(t.TempDir())
	page, err := services.GetGameGraph(db, generation.GameID)
	if err != nil {
		t.Fatal(err)
	}
	for _, part := range []string{`id="gen-slider"`, "window.generationsGraphJSON", "echarts.esm.min.js"} {
		if !strings.Contains(page, part) {
			t.Fatalf("graph page lacks %q", part)
		}
	}
}

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

func TestGameGraphWithSoldierGroups(t *testing.T) {
	db := testutils.InitTestDB(t)
	generationID := testutils.CreateGenerationForTest(t, db)
	generation, err := dal.GetGeneration(db, generationID)

	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5-1; i++ {
		_, err := services.MigrateGeneration(db, generation.GameID, []models.CommandFields{})
		if err != nil {
			t.Fatal(err)
		}
	}
	sendSoldierGroupParams := simulation.SendSoldierGroupParams{
		SoldierAmount:                 10,
		OriginPersistentCastleID:      generation.Map.Castles[0].PersistentCastleID,
		DestinationPersistentCastleID: generation.Map.Castles[1].PersistentCastleID,
	}
	rawParams, err := json.Marshal(sendSoldierGroupParams)
	if err != nil {
		t.Fatal(err)
	}
	migratedGenerationID, err := services.MigrateGeneration(
		db, generation.GameID, []models.CommandFields{
			{
				CommandType: models.
					SendSoldierGroup, Parameters: rawParams,
			},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	loadedMigratedGeneration, err := dal.GetGeneration(db, migratedGenerationID)
	if err != nil {
		t.Fatal(err)
	}
	if len(loadedMigratedGeneration.SoldierGroups) < 1 {
		t.Fatal("Sent Soldier group not loaded")
	}

	for i := 0; i < 4; i++ {
		_, err := services.MigrateGeneration(db, generation.GameID, []models.CommandFields{})
		if err != nil {
			t.Fatal(err)
		}
	}
	sendSoldierGroupParams = simulation.SendSoldierGroupParams{
		SoldierAmount:                 20,
		OriginPersistentCastleID:      generation.Map.Castles[1].PersistentCastleID,
		DestinationPersistentCastleID: generation.Map.Castles[0].PersistentCastleID,
	}
	rawParams, err = json.Marshal(sendSoldierGroupParams)
	if err != nil {
		t.Fatal(err)
	}
	_, err = services.MigrateGeneration(
		db, generation.GameID, []models.CommandFields{
			{
				CommandType: models.
					SendSoldierGroup, Parameters: rawParams,
			},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 10; i++ {
		_, err := services.MigrateGeneration(db, generation.GameID, []models.CommandFields{})
		if err != nil {
			t.Fatal(err)
		}
	}
	_ = testutils.CreateGameGraphForTest(t, db, generation.GameID)
}
