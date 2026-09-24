package routes

import (
	"islandBattles/backend/handlers"
	"islandBattles/backend/lobbyadapter"
	"islandBattles/backend/models"
	"islandBattles/lobby/application"
	"islandBattles/lobby/auth"
	"islandBattles/lobby/domain"
	"islandBattles/lobby/httpapi"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
)

// SetupLobbyRoutes embeds reusable lobby commands and the explicit game binding.
// Centrifugo's trusted proxy endpoints are registered separately from OpenAPI.
func SetupLobbyRoutes(api huma.API, lobbyApplication domain.Application, authentication *auth.Service, gameBinding *application.GameBinding[models.BindingResult], game *handlers.LobbyGame) {
	httpapi.Register(api, lobbyadapter.WithGameStart(lobbyApplication, gameBinding), authentication, authentication)
	lobbyadapter.Register(api, gameBinding, authentication)
	api.OpenAPI().Paths["/lobby/v1/games/{game_key}/lobbies/{lobby_id}/start"].Post.Description += " Island Battles also runs an automatic game and saves its spectator replay. Retrying resumes a failed game launch."
	huma.Register(api, huma.Operation{
		OperationID: "getIslandLobbyReplay", Path: "/game/from-lobby/{lobby_id}/replay", Method: http.MethodGet,
		Summary:     "Watch the automatic Island Battles game for a started lobby",
		Description: "Current lobby members can view the same saved spectator replay. No game commands are accepted.",
		Tags:        []string{"Island Battles"}, Security: []map[string][]string{{"LobbyBearer": {}}},
	}, game.Replay)
}

func SetupRoutes(api huma.API, env *handlers.Env) {
	huma.Register(api, huma.Operation{Path: "/map/create", Method: http.MethodPost, DefaultStatus: http.StatusOK,
		Summary: "Create a Map Template."}, env.CreateMapTemplate)
	huma.Register(api, huma.Operation{Path: "/generation/create", Method: http.MethodPost, DefaultStatus: http.StatusOK,
		Summary: "Create a Generation from a Map Template."}, env.CreateGenerationFromTemplate)
	huma.Register(api, huma.Operation{Path: "/generation/migrate", Method: http.MethodPost, DefaultStatus: http.StatusOK,
		Summary:     "Migrate to the next generation in the game.",
		Description: "Clash survivors continue moving and resolve later contacts in time order. Arrived groups leave field combat. Castle arrivals use the updated castle state. Send orders require a positive soldier amount and distinct origin and destination castles."}, env.MigrateGeneration)
	huma.Register(api, huma.Operation{Path: "/game/{id}/graph", Method: http.MethodGet, DefaultStatus: http.StatusOK,
		Summary: "Get a graph visualization of the game."}, env.GetGameGraph)
}
