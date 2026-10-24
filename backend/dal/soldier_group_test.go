package dal_test

import (
	"encoding/json"
	"islandBattles/backend/dal"
	"islandBattles/backend/internal/testutils"
	"islandBattles/backend/models"
	"islandBattles/backend/services"
	"islandBattles/backend/simulation"
	"testing"
)

func TestClashRadiusDefaultAndMigration(t *testing.T) {
	database := testutils.InitTestDB(t)
	generationID := testutils.CreateGenerationForTest(t, database)
	generation, err := dal.GetGeneration(database, generationID)
	if err != nil {
		t.Fatal(err)
	}
	origin, destination := generation.Map.Castles[0], generation.Map.Castles[1]
	for _, radius := range []float64{0, .75} {
		group := models.SoldierGroup{
			SoldierGroupFields: models.SoldierGroupFields{OriginCastleID: origin.ID, DestinationCastleID: destination.ID, SoldierAmount: 2},
			GenerationID:       generationID, TeamColor: origin.TeamColor, Position: origin.Position, ClashRadius: radius,
		}
		testutils.CreateSoldierGroupForTest(t, database, &group)
	}
	migratedID, err := services.MigrateGeneration(database, generation.GameID, nil)
	if err != nil {
		t.Fatal(err)
	}
	migrated, err := dal.GetGeneration(database, migratedID)
	if err != nil {
		t.Fatal(err)
	}
	if len(migrated.SoldierGroups) != 2 {
		t.Fatalf("expected two groups: %+v", migrated.SoldierGroups)
	}
	for _, group := range migrated.SoldierGroups {
		expected := models.DefaultClashRadius
		if group.PersistentSoldierGroupID == 1 {
			expected = .75
		}
		if group.ClashRadius != expected {
			t.Fatalf("radius changed across generation: %v, want %v", group.ClashRadius, expected)
		}
	}
}

// A defeated or arrived group leaves no row in later generations, but its ID
// still appears in recorded clashes, so new groups must never inherit it.
func TestPersistentSoldierGroupIDsAreNeverReassigned(t *testing.T) {
	database := testutils.InitTestDB(t)
	generationID := testutils.CreateGenerationForTest(t, database)
	generation, err := dal.GetGeneration(database, generationID)
	if err != nil {
		t.Fatal(err)
	}
	parameters, err := json.Marshal(simulation.SendSoldierGroupParams{OriginPersistentCastleID: 0, DestinationPersistentCastleID: 1, SoldierAmount: 1})
	if err != nil {
		t.Fatal(err)
	}
	send := models.CommandFields{CommandType: models.SendSoldierGroup, Parameters: parameters}
	sentID, err := services.MigrateGeneration(database, generation.GameID, []models.CommandFields{send, send})
	if err != nil {
		t.Fatal(err)
	}
	sent, err := dal.GetGeneration(database, sentID)
	if err != nil {
		t.Fatal(err)
	}
	var departedID uint
	for _, group := range sent.SoldierGroups {
		departedID = max(departedID, group.PersistentSoldierGroupID)
	}
	for _, group := range sent.SoldierGroups {
		if group.PersistentSoldierGroupID == departedID {
			if err := database.Delete(&models.SoldierGroup{}, group.ID).Error; err != nil {
				t.Fatal(err)
			}
		}
	}
	migratedID, err := services.MigrateGeneration(database, generation.GameID, []models.CommandFields{send})
	if err != nil {
		t.Fatal(err)
	}
	migrated, err := dal.GetGeneration(database, migratedID)
	if err != nil {
		t.Fatal(err)
	}
	if len(migrated.SoldierGroups) != 2 {
		t.Fatalf("expected the remaining and the new group: %+v", migrated.SoldierGroups)
	}
	seen := map[uint]bool{}
	for _, group := range migrated.SoldierGroups {
		if group.PersistentSoldierGroupID == departedID {
			t.Fatalf("new group reused departed group ID %d", departedID)
		}
		if seen[group.PersistentSoldierGroupID] {
			t.Fatalf("duplicate group ID %d", group.PersistentSoldierGroupID)
		}
		seen[group.PersistentSoldierGroupID] = true
	}
}
