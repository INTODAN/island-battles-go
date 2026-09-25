package routes_test

import (
	"github.com/danielgtaylor/huma/v2/humatest"
	"islandBattles/backend/handlers"
	"islandBattles/backend/routes"
	"testing"
)

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
