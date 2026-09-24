package routes

import (
	"islandBattles/backend/handlers"
	"islandBattles/backend/lobbyadapter"
	"islandBattles/lobby"
	"islandBattles/lobby/auth"
	"islandBattles/lobby/httpapi"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
)

// SetupLobbyRoutes embeds reusable lobby commands and the explicit game binding.
// Centrifugo's trusted proxy endpoints are registered separately from OpenAPI.
func SetupLobbyRoutes(api huma.API, application lobby.Application, authentication *auth.Service, gameAdapter *lobbyadapter.Service) {
	httpapi.Register(api, application, authentication, authentication)
	lobbyadapter.Register(api, gameAdapter, authentication)
}

func SetupRoutes(api huma.API, env *handlers.Env) {
	huma.Register(api, huma.Operation{Path: "/map/create", Method: http.MethodPost, DefaultStatus: http.StatusOK,
		Summary: "Create a Map Template."}, env.CreateMapTemplate)
	huma.Register(api, huma.Operation{Path: "/generation/create", Method: http.MethodPost, DefaultStatus: http.StatusOK,
		Summary: "Create a Generation from a Map Template."}, env.CreateGenerationFromTemplate)
	huma.Register(api, huma.Operation{Path: "/generation/migrate", Method: http.MethodPost, DefaultStatus: http.StatusOK,
		Summary: "Migrate to the next generation in the game."}, env.MigrateGeneration)
	huma.Register(api, huma.Operation{Path: "/game/{id}/graph", Method: http.MethodGet, DefaultStatus: http.StatusOK,
		Summary: "Get a graph visualization of the game."}, env.GetGameGraph)
}
