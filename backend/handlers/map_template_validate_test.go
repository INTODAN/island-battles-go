package handlers_test

import (
	"encoding/json"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2/humatest"
	"islandBattles/backend/handlers"
	"islandBattles/backend/internal/testutils"
	"islandBattles/backend/routes"
)

type validation struct {
	Valid  bool `json:"valid"`
	Errors []struct {
		Location string `json:"location"`
		Message  string `json:"message"`
	} `json:"errors"`
}

func TestValidateMapTemplateHTTP(t *testing.T) {
	// No database: checking a map stores nothing.
	_, api := humatest.New(t, routes.NewAPIConfig("test", "1.0.0"))
	routes.SetupRoutes(api, &handlers.Env{})
	check := func(t *testing.T, body string) validation {
		t.Helper()
		response := api.Post("/map/validate", strings.NewReader(body))
		if response.Code != http.StatusOK {
			t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
		}
		var result validation
		if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		if result.Errors == nil {
			t.Fatalf("errors is not a list: %s", response.Body.String())
		}
		return result
	}
	locations := func(result validation) []string {
		found := []string{}
		for _, problem := range result.Errors {
			found = append(found, problem.Location)
		}
		return found
	}

	t.Run("valid", func(t *testing.T) {
		if result := check(t, testutils.MapExampleInput); !result.Valid || len(result.Errors) != 0 {
			t.Fatalf("example map rejected: %+v", result)
		}
	})
	t.Run("rule problems are located at their castles", func(t *testing.T) {
		result := check(t, `{"map_name":"invalid","player_amount":2,"plane_size_x":4,"plane_size_y":4,"neutral_starting_soldier_amount":10,
			"castles":[{"team_color":3,"x":0,"y":0},{"team_color":1,"x":2.5,"y":-1,"upgrades":{"speed":9}}]}`)
		if expected := []string{"castles[0].team_color", "castles[1].x", "castles[1].upgrades.speed"}; result.Valid || !reflect.DeepEqual(locations(result), expected) {
			t.Fatalf("result=%+v, want locations %v", result, expected)
		}
	})
	t.Run("strict decoding", func(t *testing.T) {
		for name, test := range map[string]struct{ body, location string }{
			"unknown key":   {`{"map_name":"a","player_amount":1,"plane_size_x":2,"plane_size_y":2,"neutral_starting_soldier_amount":1,"castles":[{"team_color":1,"x":0,"y":0,"position":"(0,0)"}]}`, "castles[0].position"},
			"missing key":   {`{"map_name":"a","player_amount":1,"plane_size_x":2,"plane_size_y":2,"neutral_starting_soldier_amount":1,"castles":[{"team_color":1,"y":0}]}`, "castles[0]"},
			"schema link":   {`{"$schema":"x","map_name":"a","player_amount":1,"plane_size_x":2,"plane_size_y":2,"neutral_starting_soldier_amount":1,"castles":[{"team_color":1,"x":0,"y":0}]}`, "$schema"},
			"malformed":     {`{"map_name":`, ""},
			"not an object": {`[]`, ""},
		} {
			t.Run(name, func(t *testing.T) {
				if result := check(t, test.body); result.Valid || len(result.Errors) != 1 || result.Errors[0].Location != test.location {
					t.Fatalf("result=%+v, want one problem at %q", result, test.location)
				}
			})
		}
	})
	t.Run("an empty body is a bad request", func(t *testing.T) {
		if response := api.Post("/map/validate", strings.NewReader("")); response.Code != http.StatusBadRequest {
			t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
		}
	})
}
