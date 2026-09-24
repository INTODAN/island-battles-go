package routes

import (
	"encoding/json"
	"islandBattles/backend/handlers"
	"islandBattles/backend/lobbyadapter"
	"islandBattles/backend/models"
	"islandBattles/lobby/application"
	"islandBattles/lobby/auth"
	"islandBattles/lobby/domain"
	"islandBattles/lobby/httpapi"
	"net/http"
	"reflect"

	"github.com/danielgtaylor/huma/v2"
	"gorm.io/datatypes"
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
	// GORM JSON marshals as raw JSON, rather than the base64 string Huma infers
	// from its underlying byte slice. Describe command parameters accordingly.
	api.OpenAPI().Components.Schemas.RegisterTypeAlias(reflect.TypeOf(datatypes.JSON{}), reflect.TypeOf(json.RawMessage{}))
	huma.Register(api, huma.Operation{Path: "/map/create", Method: http.MethodPost, DefaultStatus: http.StatusOK,
		Summary: "Create a map template with optional castle upgrades.", Description: "Castle upgrades are a map of upgrade type to level. Omitted upgrades default to level 1; speed supports levels 1 through 4."}, env.CreateMapTemplate)
	huma.Register(api, huma.Operation{Path: "/generation/create", Method: http.MethodPost, DefaultStatus: http.StatusOK,
		Summary: "Create a Generation from a Map Template."}, env.CreateGenerationFromTemplate)
	huma.Register(api, huma.Operation{Path: "/generation/migrate", Method: http.MethodPost, DefaultStatus: http.StatusOK,
		Summary: "Advance one game turn and apply commands, including castle upgrades.", Description: "Commands execute in order. upgrade_castle accepts logical_castle_id and upgrade_type, and advances that upgrade by one level, atomically deducting its fixed soldier cost from the castle. Speed levels 2, 3, and 4 cost 10, 20, and 30 soldiers. Insufficient soldiers returns HTTP 409. Groups spawned afterward copy the castle upgrades; existing groups retain their levels. A rejected command rolls back the entire turn. Clashes resolve in time order. Survivors continue moving only when one group has strictly less than half the other's troops; otherwise they stop at the contact midpoint. ClashStopGenerations configures the pause, including the clash generation (default 1). Arrived groups leave field combat. Castle arrivals use the updated castle state. Send orders require a positive soldier amount and distinct origin and destination castles."}, env.MigrateGeneration)
	huma.Register(api, huma.Operation{Path: "/game/{id}/graph", Method: http.MethodGet, DefaultStatus: http.StatusOK,
		Summary: "Get a graph visualization of the game."}, env.GetGameGraph)
}
