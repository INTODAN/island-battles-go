package dal_test

import (
	"encoding/json"
	"islandBattles/backend/dal"
	"islandBattles/backend/internal/testutils"
	"islandBattles/backend/models"
	"testing"
)

func TestCreateMapTemplate(t *testing.T) {
	db := testutils.InitTestDB(t)

	var mapTemplate models.MapTemplate
	err := json.Unmarshal([]byte(testutils.MapExampleInput), &mapTemplate)
	if err != nil {
		t.Fatalf("Failed to unmarshal MapExampleInput: %v", err)
	}

	// First creation should succeed
	id, err := dal.CreateMapTemplate(db, &mapTemplate)
	if err != nil {
		t.Fatalf("Expected no error on first creation, got: %v", err)
	}
	if id == 0 {
		t.Fatal("Expected non-zero ID on first creation")
	}

	// Second creation should fail due to duplicate name
	_, err = dal.CreateMapTemplate(db, &mapTemplate)
	if err == nil {
		t.Fatal("Expected error on second creation (duplicate name), got nil")
	}
}
