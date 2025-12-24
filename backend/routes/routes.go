package routes

import (
	"islandBattles/backend/handlers"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
)

func SetupRoutes(api *huma.API, env *handlers.Env) {
	huma.Register(*api, huma.Operation{Path: "/ping", Method: http.MethodPost, DefaultStatus: http.StatusOK}, env.Ping)
	huma.Register(*api, huma.Operation{Path: "/connect", Method: http.MethodPost, DefaultStatus: http.StatusOK},
		env.Connect)
	huma.Register(*api, huma.Operation{Path: "/map/create", Method: http.MethodPost, DefaultStatus: http.StatusOK,
		Summary: "Create a Map Template."}, env.CreateMapTemplate)
	huma.Register(*api, huma.Operation{Path: "/generation/create", Method: http.MethodPost, DefaultStatus: http.StatusOK,
		Summary: "Create a Generation from a Map Template."}, env.CreateGenerationFromTemplate)
	huma.Register(*api, huma.Operation{Path: "/generation/migrate", Method: http.MethodPost, DefaultStatus: http.StatusOK,
		Summary: "Migrate to the next generation in the game."}, env.MigrateGeneration)
	huma.Register(*api, huma.Operation{Path: "/game/{id}/graph", Method: http.MethodGet, DefaultStatus: http.StatusOK,
		Summary: "Get a graph visualization of the game's latest generation."}, env.GetGameGraph)
}
