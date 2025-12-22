package testutils

import (
	"encoding/json"
	"islandBattles/backend/dal"
	"islandBattles/backend/models"
	"testing"

	"gorm.io/gorm"
)

func GetExampleMapTemplate(t *testing.T, db *gorm.DB) uint {
	var mapTemplate models.MapTemplate
	err := json.Unmarshal([]byte(MapExampleInput), &mapTemplate)
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
	mapTemplateID := GetExampleMapTemplate(t, db)
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
