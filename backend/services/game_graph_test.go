package services_test

import (
	"encoding/json"
	"islandBattles/backend/dal"
	"islandBattles/backend/dal/commandHandlers"
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
	sendSoldierGroupParams := commandHandlers.SendSoldierGroupParams{SoldierGroupFields: models.
		SoldierGroupFields{SoldierAmount: 10, OriginCastleID: generation.Map.Castles[0].LogicalCastleID,
		DestinationCastleID: generation.Map.Castles[1].LogicalCastleID}}
	rawParams, err := json.Marshal(sendSoldierGroupParams)
	if err != nil {
		t.Fatal(err)
	}
	migratedGenerationID, err := services.MigrateGeneration(db, generation.GameID, []models.CommandFields{{CommandType: models.
		SendSoldierGroup, Parameters: rawParams}})
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
	sendSoldierGroupParams = commandHandlers.SendSoldierGroupParams{SoldierGroupFields: models.
		SoldierGroupFields{SoldierAmount: 20, OriginCastleID: generation.Map.Castles[1].LogicalCastleID,
		DestinationCastleID: generation.Map.Castles[0].LogicalCastleID}}
	rawParams, err = json.Marshal(sendSoldierGroupParams)
	if err != nil {
		t.Fatal(err)
	}
	_, err = services.MigrateGeneration(db, generation.GameID, []models.CommandFields{{CommandType: models.
		SendSoldierGroup, Parameters: rawParams}})
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
