package dal_test

import (
	"islandBattles/backend/dal"
	"islandBattles/backend/dal/commandHandlers"
	"islandBattles/backend/internal/testutils"
	"islandBattles/backend/models"
	"islandBattles/backend/services"
	"testing"
)

func TestSoldierGroupColor(t *testing.T) {
	db := testutils.InitTestDB(t)
	generationID := testutils.CreateGenerationForTest(t, db)

	generation, err := dal.GetGeneration(db, generationID)
	if err != nil {
		t.Fatal(err)
	}

	// Castle A (Logical ID 0) sends soldier group to Castle B (Logical ID 1)
	// MapExampleInput: {"castles": [{"TeamColor": 1, "Position": "(0,1)"},{"TeamColor": 2, "Position": "(3,1)"}], ...}

	castleA, err := dal.GetCastleFromGeneration(generation, 0)
	if err != nil {
		t.Fatal(err)
	}
	initialColor := castleA.TeamColor

	params := commandHandlers.SendSoldierGroupParams{
		OriginLogicalCastleID:      0,
		DestinationLogicalCastleID: 1,
		SoldierAmount:              5,
	}

	err = commandHandlers.SendSoldierGroupHandler(db, generation, params)
	if err != nil {
		t.Fatal(err)
	}

	// Verify soldier group was created and has initialColor
	generation, err = dal.GetGeneration(db, generationID) // reload to get soldier groups
	if err != nil {
		t.Fatal(err)
	}
	if len(generation.SoldierGroups) != 1 {
		t.Fatalf("Expected 1 soldier group, got %d", len(generation.SoldierGroups))
	}
	if generation.SoldierGroups[0].TeamColor != initialColor {
		t.Fatalf("Expected soldier group color %d, got %d", initialColor, generation.SoldierGroups[0].TeamColor)
	}

	// Migrate to next generation
	nextGenerationID, err := services.MigrateGeneration(db, generation.GameID, []models.CommandFields{})
	if err != nil {
		t.Fatal(err)
	}

	nextGeneration, err := dal.GetGeneration(db, nextGenerationID)
	if err != nil {
		t.Fatal(err)
	}

	// Change Castle A color in the NEW generation (simulating capture)
	nextCastleA, err := dal.GetCastleFromGeneration(nextGeneration, 0)
	if err != nil {
		t.Fatal(err)
	}
	newColor := uint(3)
	err = dal.UpdateCastleTeamColor(db, nextCastleA, newColor)
	if err != nil {
		t.Fatal(err)
	}

	// Verify Soldier Group in next generation still has initialColor
	if len(nextGeneration.SoldierGroups) != 1 {
		t.Fatalf("Expected 1 soldier group in next generation, got %d", len(nextGeneration.SoldierGroups))
	}
	if nextGeneration.SoldierGroups[0].TeamColor != initialColor {
		t.Fatalf("Expected soldier group color in next generation to be %d, got %d", initialColor, nextGeneration.SoldierGroups[0].TeamColor)
	}
}
