package routes_test

import (
	"github.com/danielgtaylor/huma/v2/humatest"
	"islandBattles/backend/handlers"
	"islandBattles/backend/routes"
	"testing"
)

func TestMapCreateAcceptsTheMapFileFormat(t *testing.T) {
	_, api := humatest.New(t, routes.NewAPIConfig("test", "1.0.0"))
	routes.SetupRoutes(api, &handlers.Env{})
	body := api.OpenAPI().Paths["/map/create"].Post.RequestBody.Content["application/json"].Schema
	if body.Ref != "#/components/schemas/MapTemplate" {
		t.Fatalf("/map/create body is %q, not the MapTemplate schema", body.Ref)
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
