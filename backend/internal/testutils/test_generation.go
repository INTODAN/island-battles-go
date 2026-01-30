package testutils

import (
	"encoding/json"
	"islandBattles/backend/dal"
	"islandBattles/backend/models"
	"testing"

	"gorm.io/gorm"
)

func getExampleMapTemplate(t *testing.T, db *gorm.DB, mapInput string) uint {
	var mapTemplate models.MapTemplate
	err := json.Unmarshal([]byte(mapInput), &mapTemplate)
	if err != nil {
		t.Fatal(err)
	}
	mapTemplateID, err := dal.CreateMapTemplate(db, &mapTemplate)
	if err != nil {
		t.Fatal(err)
	}
	return mapTemplateID
}

func CreateGenerationForTest(t *testing.T, db *gorm.DB) uint {
	return createGenerationForTest(t, db, MapExampleInput)
}

func CreateComplexGenerationForTest(t *testing.T, db *gorm.DB) uint {
	return createGenerationForTest(t, db, MapExampleInputComplex)
}

func createGenerationForTest(t *testing.T, db *gorm.DB, mapInput string) uint {
	mapTemplateID := getExampleMapTemplate(t, db, mapInput)
	gameID, err := dal.CreateGame(db)
	if err != nil {
		t.Fatal(err)
	}
	generationID, err := dal.CreateGenerationFromTemplate(db, mapTemplateID, gameID)
	if err != nil {
		t.Fatal(err)
	}
	return generationID
}
