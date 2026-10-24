package handlers_test

import (
	"encoding/json"
	"net/http"
	"reflect"
	"testing"

	"github.com/danielgtaylor/huma/v2/humatest"
	"islandBattles/backend/handlers"
	"islandBattles/backend/internal/testutils"
	"islandBattles/backend/models"
	"islandBattles/backend/routes"
)

func TestCreateMapTemplateHTTP(t *testing.T) {
	database := testutils.InitTestDB(t)
	_, api := humatest.New(t)
	routes.SetupRoutes(api, &handlers.Env{DB: database})

	invalid := api.Post("/map/create", map[string]any{
		"map_name": "invalid", "player_amount": 2, "plane_size_x": 4, "plane_size_y": 4, "neutral_starting_soldier_amount": 10,
		"castles": []map[string]any{
			{"team_color": 3, "x": 0, "y": 0},
			{"team_color": 1, "x": 2.5, "y": -1, "upgrades": map[string]int{"speed": 9}},
		},
	})
	if invalid.Code != http.StatusUnprocessableEntity {
		t.Fatalf("invalid map status=%d body=%s", invalid.Code, invalid.Body.String())
	}
	var problem struct {
		Errors []struct{ Location string } `json:"errors"`
	}
	if err := json.Unmarshal(invalid.Body.Bytes(), &problem); err != nil {
		t.Fatal(err)
	}
	locations := []string{}
	for _, detail := range problem.Errors {
		locations = append(locations, detail.Location)
	}
	if expected := []string{"body.castles[0].team_color", "body.castles[1].x", "body.castles[1].upgrades.speed"}; !reflect.DeepEqual(locations, expected) {
		t.Fatalf("locations=%v, want %v", locations, expected)
	}
	var stored int64
	if err := database.Model(&models.MapTemplate{}).Count(&stored).Error; err != nil || stored != 0 {
		t.Fatalf("invalid map stored: count=%d err=%v", stored, err)
	}

	created := api.Post("/map/create", json.RawMessage(testutils.MapExampleInput))
	if created.Code != http.StatusOK {
		t.Fatalf("valid map status=%d body=%s", created.Code, created.Body.String())
	}
	var result struct {
		MapTemplateID uint `json:"map_template_id"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &result); err != nil || result.MapTemplateID == 0 {
		t.Fatalf("unexpected result %s: %v", created.Body.String(), err)
	}

	if duplicate := api.Post("/map/create", json.RawMessage(testutils.MapExampleInput)); duplicate.Code != http.StatusConflict {
		t.Fatalf("duplicate name status=%d body=%s", duplicate.Code, duplicate.Body.String())
	}
}
