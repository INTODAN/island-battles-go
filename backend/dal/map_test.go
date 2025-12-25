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

	// Verify that the map template and its castle positions were saved correctly
	var savedTemplate models.MapTemplate
	if err := db.Preload("CastleTemplates").First(&savedTemplate, id).Error; err != nil {
		t.Fatalf("Failed to retrieve saved map template: %v", err)
	}

	if len(savedTemplate.CastleTemplates) != 2 {
		t.Fatalf("Expected 2 castles, got %d", len(savedTemplate.CastleTemplates))
	}

	// Check first castle position (expected (0,1) from MapExampleInput)
	expectedX, expectedY := 0.0, 1.0
	if savedTemplate.CastleTemplates[0].Position.P.X != expectedX || savedTemplate.CastleTemplates[0].Position.P.Y != expectedY {
		t.Errorf("Expected first castle position (%f, %f), got (%f, %f)",
			expectedX, expectedY,
			savedTemplate.CastleTemplates[0].Position.P.X,
			savedTemplate.CastleTemplates[0].Position.P.Y)
	}

	// Second creation should fail due to duplicate name
	_, err = dal.CreateMapTemplate(db, &mapTemplate)
	if err == nil {
		t.Fatal("Expected error on second creation (duplicate name), got nil")
	}
}
