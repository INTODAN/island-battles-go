package routes_test

import (
	"github.com/danielgtaylor/huma/v2/humatest"
	"islandBattles/backend/handlers"
	"islandBattles/backend/routes"
	"net/http"
	"strings"
	"testing"
)

func TestMapCreateAcceptsTheMapFileFormat(t *testing.T) {
	_, api := humatest.New(t, routes.NewAPIConfig("test", "1.0.0"))
	routes.SetupRoutes(api, &handlers.Env{})
	body := api.OpenAPI().Paths["/map/create"].Post.RequestBody.Content["application/json"].Schema
	if body.Ref != "#/components/schemas/MapTemplate" {
		t.Fatalf("/map/create body is %q, not the MapTemplate schema", body.Ref)
	}
	if _, linked := api.OpenAPI().Components.Schemas.SchemaFromRef(body.Ref).Properties["$schema"]; linked {
		t.Fatal("the map file format publishes Huma's $schema link property")
	}
	// Validation rejects the body before the handler would reach the database.
	linkedFile := `{"$schema":"https://example.com/MapTemplate.json","map_name":"linked","player_amount":1,"plane_size_x":2,"plane_size_y":2,"neutral_starting_soldier_amount":1,"castles":[{"team_color":1,"x":0,"y":0}]}`
	if response := api.Post("/map/create", strings.NewReader(linkedFile)); response.Code != http.StatusUnprocessableEntity || !strings.Contains(response.Body.String(), "body.$schema") {
		t.Fatalf("a map file with $schema got status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestRawGenerationMutationRoutesAreRemoved(t *testing.T) {
	_, api := humatest.New(t)
	routes.SetupRoutes(api, &handlers.Env{})
	for _, path := range []string{"/generation/create", "/generation/migrate"} {
		if _, exists := api.OpenAPI().Paths[path]; exists {
			t.Fatalf("public bypass route remains: %s", path)
		}
		if response := api.Post(path, `{}`); response.Code != 404 {
			t.Fatalf("%s status=%d", path, response.Code)
		}
	}
}
