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
	// Create a complex game
	generationID := testutils.CreateComplexGenerationForTest(t, db)

	generation, err := dal.GetGeneration(db, generationID)
	if err != nil {
		t.Fatal(err)
	}

	// Castle 0 (Team 1) is at (0, 1).
	// Castle 1 (Team 2) is at (4, 1).
	// Castle 2 (Team 2) is at (2, 2).

	castle0, err := dal.GetCastleFromGeneration(generation, 0)
	if err != nil {
		t.Fatal(err)
	}
	initialColor := castle0.TeamColor

	// Castle 2 needs enough soldiers to successfully attack and capture Castle 0 later.
	castle2, err := dal.GetCastleFromGeneration(generation, 2)
	if err != nil {
		t.Fatal(err)
	}
	err = dal.UpdateCastleSoldierGroupAmount(db, castle2, 40)
	if err != nil {
		t.Fatal(err)
	}

	// Reload generation to apply starting updates
	generation, err = dal.GetGeneration(db, generationID)
	if err != nil {
		t.Fatal(err)
	}

	// Step 0: Castle 0 (Team 1) sends Group X (5 soldiers) to Castle 1 (Team 2).
	// Group X allegiance is Team 1. Distance is 4.0, taking 8 migrations to arrive.
	paramsX := commandHandlers.SendSoldierGroupParams{
		OriginLogicalCastleID:      0,
		DestinationLogicalCastleID: 1,
		SoldierAmount:              5,
	}
	err = commandHandlers.SendSoldierGroupHandler(db, generation, paramsX)
	if err != nil {
		t.Fatal(err)
	}

	// Migrate to Gen 1
	currentGameID := generation.GameID
	gen1ID, err := services.MigrateGeneration(db, currentGameID, []models.CommandFields{})
	if err != nil {
		t.Fatal(err)
	}
	gen1, err := dal.GetGeneration(db, gen1ID)
	if err != nil {
		t.Fatal(err)
	}

	// Step 1: Castle 2 (Team 2) sends Group Y (30 soldiers) to Castle 0 (Team 1).
	// Group Y is sent from (2, 2) to (0, 1), taking 5 migrations to arrive.
	paramsY := commandHandlers.SendSoldierGroupParams{
		OriginLogicalCastleID:      2,
		DestinationLogicalCastleID: 0,
		SoldierAmount:              30,
	}
	err = commandHandlers.SendSoldierGroupHandler(db, gen1, paramsY)
	if err != nil {
		t.Fatal(err)
	}

	// Migrate 5 more times so that Group Y reaches Castle 0 at Generation 6.
	currentGenID := gen1ID
	for i := 0; i < 5; i++ {
		currentGenID, err = services.MigrateGeneration(db, currentGameID, []models.CommandFields{})
		if err != nil {
			t.Fatal(err)
		}
	}

	gen6, err := dal.GetGeneration(db, currentGenID)
	if err != nil {
		t.Fatal(err)
	}

	// Verify that Castle 0 has been captured by Team 2 (its TeamColor is now 2)
	capturedCastle0, err := dal.GetCastleFromGeneration(gen6, 0)
	if err != nil {
		t.Fatal(err)
	}
	if capturedCastle0.TeamColor != 2 {
		t.Fatalf("Expected Castle 0 to be captured by Team 2, but got Team %d", capturedCastle0.TeamColor)
	}

	// Verify that Group X (still in flight from Castle 0 to Castle 1) still retains its original TeamColor (Team 1)
	var groupX *models.SoldierGroup
	for i := range gen6.SoldierGroups {
		sg := &gen6.SoldierGroups[i]
		if sg.OriginCastle.LogicalCastleID == 0 && sg.DestinationCastle.LogicalCastleID == 1 {
			groupX = sg
			break
		}
	}

	if groupX == nil {
		t.Fatalf("Expected Group X to be in flight, but it was not found")
	}

	if groupX.TeamColor != initialColor {
		t.Fatalf("Allegiance mismatch: Group X team color was changed to %d; expected original color %d", groupX.TeamColor, initialColor)
	}

	// Migrate to Gen 7 (just before Group X arrives at Castle 1)
	gen7ID, err := services.MigrateGeneration(db, currentGameID, []models.CommandFields{})
	if err != nil {
		t.Fatal(err)
	}
	gen7, err := dal.GetGeneration(db, gen7ID)
	if err != nil {
		t.Fatal(err)
	}
	castle1Gen7, err := dal.GetCastleFromGeneration(gen7, 1)
	if err != nil {
		t.Fatal(err)
	}
	soldiersBeforeArrival := castle1Gen7.SoldierAmount

	// Migrate to Gen 8 (Group X arrives at Castle 1 and resolves battle/reduction)
	gen8ID, err := services.MigrateGeneration(db, currentGameID, []models.CommandFields{})
	if err != nil {
		t.Fatal(err)
	}
	gen8, err := dal.GetGeneration(db, gen8ID)
	if err != nil {
		t.Fatal(err)
	}
	castle1Gen8, err := dal.GetCastleFromGeneration(gen8, 1)
	if err != nil {
		t.Fatal(err)
	}
	soldiersAfterArrival := castle1Gen8.SoldierAmount

	// Verify that the destination castle's garrison size decreased
	if soldiersAfterArrival >= soldiersBeforeArrival {
		t.Fatalf("Expected destination castle soldier amount to decrease, but before was %d and after was %d", soldiersBeforeArrival, soldiersAfterArrival)
	}

	// Castle 1 had 22 soldiers in Gen 7.
	// In Gen 8, it receives +1 passive generation (total 23), but gets attacked by Group X (5 soldiers).
	// Remaining soldiers = 23 - 5 = 18.
	expectedRemaining := uint(18)
	if soldiersAfterArrival != expectedRemaining {
		t.Fatalf("Expected destination castle soldier amount after attack to be %d, got %d", expectedRemaining, soldiersAfterArrival)
	}

	// Export the game graph so a human can inspect the full capture timeline
	_ = testutils.CreateGameGraphForTest(t, db, currentGameID)
}
