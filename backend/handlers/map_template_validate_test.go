package handlers_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2/humatest"
	"islandBattles/backend/handlers"
	"islandBattles/backend/internal/testutils"
	"islandBattles/backend/models"
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
	t.Run("a map file as the map builder downloads it", func(t *testing.T) {
		// builder.mjs serialize: fixed key order, two-space indent, optional keys only when set.
		builderFile := `{
  "map_name": "Test crossing",
  "player_amount": 2,
  "plane_size_x": 16,
  "plane_size_y": 10,
  "neutral_starting_soldier_amount": 10,
  "castles": [
    {
      "team_color": 1,
      "x": -5.3,
      "y": 0,
      "starting_soldier_amount": 30,
      "upgrades": {
        "clash_stop_generations": 2,
        "speed": 4
      }
    },
    {
      "team_color": 2,
      "x": 8,
      "y": -5,
      "starting_soldier_amount": 0
    },
    {
      "team_color": 0,
      "x": 0,
      "y": 0
    }
  ]
}
`
		if result := check(t, builderFile); !result.Valid || len(result.Errors) != 0 {
			t.Fatalf("builder file rejected: %+v", result)
		}
	})
	t.Run("many unknown keys get a small answer", func(t *testing.T) {
		// Huma reports an unknown key with the whole enclosing object as its
		// value. Echoing it once per key made the answer grow with the square
		// of the number of keys: 400 keys answered 1.48 MB.
		const unknownKeys = 2000
		var body strings.Builder
		body.WriteString(`{"map_name":"a","player_amount":1,"plane_size_x":2,"plane_size_y":2,"neutral_starting_soldier_amount":1,"castles":[{"team_color":1,"x":0,"y":0}]`)
		for key := range unknownKeys {
			fmt.Fprintf(&body, `,"unknown_%d":%d`, key, key)
		}
		body.WriteString("}")
		runtime.GC()
		var before, after runtime.MemStats
		runtime.ReadMemStats(&before)
		response := api.Post("/map/validate", strings.NewReader(body.String()))
		runtime.ReadMemStats(&after)
		if response.Code != http.StatusOK {
			t.Fatalf("status=%d", response.Code)
		}
		size, allocated := response.Body.Len(), after.TotalAlloc-before.TotalAlloc
		t.Logf("%d unknown keys: answered %d bytes, allocated %d bytes", unknownKeys, size, allocated)
		if size > 16<<10 || allocated > 16<<20 {
			t.Fatalf("answered %d bytes and allocated %d bytes, over 16 KiB or 16 MiB", size, allocated)
		}
		var result validation
		if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		last := result.Errors[len(result.Errors)-1]
		if result.Valid || len(result.Errors) != models.MaxMapTemplateProblems+1 || last.Location != "" || last.Message != fmt.Sprintf("and %d more problems", unknownKeys-models.MaxMapTemplateProblems) {
			t.Fatalf("valid=%v, %d errors, last=%+v", result.Valid, len(result.Errors), last)
		}
		if !strings.HasPrefix(result.Errors[0].Location, "unknown_") || strings.Contains(response.Body.String(), `"value"`) {
			t.Fatalf("an unknown key is not reported by name alone: %s", response.Body.String()[:200])
		}
	})
	t.Run("an empty body is a bad request", func(t *testing.T) {
		if response := api.Post("/map/validate", strings.NewReader("")); response.Code != http.StatusBadRequest {
			t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
		}
	})
}
