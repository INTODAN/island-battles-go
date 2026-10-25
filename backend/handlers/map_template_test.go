package handlers_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2/humatest"
	"islandBattles/backend/handlers"
	"islandBattles/backend/internal/testutils"
	"islandBattles/backend/models"
	"islandBattles/backend/routes"
)

const testAdminAPIKey = "test-admin-api-key"

var adminHeader = routes.AdminAPIKeyHeader + ": " + testAdminAPIKey

func TestCreateMapTemplateHTTP(t *testing.T) {
	database := testutils.InitTestDB(t)
	_, api := humatest.New(t)
	routes.SetupRoutes(api, &handlers.Env{DB: database}, testAdminAPIKey)

	invalid := api.Post("/map/create", adminHeader, map[string]any{
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

	created := api.Post("/map/create", adminHeader, json.RawMessage(testutils.MapExampleInput))
	if created.Code != http.StatusOK {
		t.Fatalf("valid map status=%d body=%s", created.Code, created.Body.String())
	}
	var result struct {
		MapTemplateID uint `json:"map_template_id"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &result); err != nil || result.MapTemplateID == 0 {
		t.Fatalf("unexpected result %s: %v", created.Body.String(), err)
	}

	if duplicate := api.Post("/map/create", adminHeader, json.RawMessage(testutils.MapExampleInput)); duplicate.Code != http.StatusConflict {
		t.Fatalf("duplicate name status=%d body=%s", duplicate.Code, duplicate.Body.String())
	}
}

// A lobby has at most 16 teams, so a map for 17 fits none.
var seventeenTeamMap = func() string {
	castles := make([]string, 17)
	for index := range castles {
		castles[index] = fmt.Sprintf(`{"team_color":%d,"x":0,"y":0}`, index+1)
	}
	return `{"map_name":"Crowd","player_amount":17,"plane_size_x":4,"plane_size_y":4,"neutral_starting_soldier_amount":5,"castles":[` + strings.Join(castles, ",") + `]}`
}()

func TestListMapTemplatesHTTP(t *testing.T) {
	database := testutils.InitTestDB(t)
	_, api := humatest.New(t)
	routes.SetupRoutes(api, &handlers.Env{DB: database}, testAdminAPIKey)

	empty := api.Get("/map/templates")
	if empty.Code != http.StatusOK || strings.Join(strings.Fields(empty.Body.String()), "") != `{"maps":[]}` {
		t.Fatalf("empty list status=%d body=%s", empty.Code, empty.Body.String())
	}
	for _, mapFile := range []string{
		`{"map_name":"Zigzag","player_amount":3,"plane_size_x":12,"plane_size_y":9,"neutral_starting_soldier_amount":5,"castles":[{"team_color":1,"x":0,"y":0},{"team_color":2,"x":1,"y":0},{"team_color":3,"x":2,"y":0},{"team_color":0,"x":3,"y":0}]}`,
		testutils.MapExampleInput,
		// A lobby has at least two teams.
		`{"map_name":"Solo","player_amount":1,"plane_size_x":4,"plane_size_y":4,"neutral_starting_soldier_amount":5,"castles":[{"team_color":1,"x":0,"y":0}]}`,
		seventeenTeamMap,
		// Colour 2 owns no castle, so the map fits no lobby.
		`{"map_name":"Gap","player_amount":3,"plane_size_x":4,"plane_size_y":4,"neutral_starting_soldier_amount":5,"castles":[{"team_color":1,"x":0,"y":0},{"team_color":3,"x":1,"y":0}]}`,
	} {
		if created := api.Post("/map/create", adminHeader, json.RawMessage(mapFile)); created.Code != http.StatusOK {
			t.Fatalf("create status=%d body=%s", created.Code, created.Body.String())
		}
	}
	listed := api.Get("/map/templates")
	var result struct {
		Maps []models.MapTemplateSummaryView `json:"maps"`
	}
	if err := json.Unmarshal(listed.Body.Bytes(), &result); err != nil || listed.Code != http.StatusOK {
		t.Fatalf("list status=%d body=%s err=%v", listed.Code, listed.Body.String(), err)
	}
	for index := range result.Maps {
		result.Maps[index].MapTemplateID = 0
	}
	expected := []models.MapTemplateSummaryView{
		{MapName: "Zigzag", TeamCount: 3, PlaneSizeX: 12, PlaneSizeY: 9, CastleCount: 4},
		{MapName: "my-first-map", TeamCount: 2, PlaneSizeX: 8, PlaneSizeY: 6, CastleCount: 2},
	}
	if !reflect.DeepEqual(result.Maps, expected) {
		t.Fatalf("maps = %+v, want %+v", result.Maps, expected)
	}
}
