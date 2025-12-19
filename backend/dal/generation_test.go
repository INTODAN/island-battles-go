package dal

import (
	"encoding/json"
	"islandBattles/backend/internal/testutils"
	"islandBattles/backend/models"
	"testing"
)

const MapExampleInput = `{"castles": [{"TeamColor": 0, "position": "(0,1)"},{"TeamColor": 1, "position": "(3,1)"}], "map_name": "my-first-map", "neutral_starting_soldier_amount": 15,"plane_size_x": 4, "plane_size_y": 3, "player_amount": 2}`

func TestCreateGeneration(t *testing.T) {
	db := testutils.InitTestDB(t)
	// Create the map template struct out of the MapExampleInput const
	var mapTemplate models.MapTemplate
	err := json.Unmarshal([]byte(MapExampleInput), &mapTemplate)
	if err != nil {
		t.Fatal(err)
	}
	mapTemplateId, err := CreateMapTemplate(db, &mapTemplate)
	if err != nil {
		t.Fatal(err)
	}
	_, err = CreateGenerationFromTemplate(db, mapTemplateId)
	if err != nil {
		t.Fatal(err)
	}
}
