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
	api.OpenAPI().Paths["/lobby/v1/games/{game_key}/lobbies/{lobby_id}/start"].Post.Description += " Island Battles initializes one playable generation and waits for every frozen roster player to complete their turn. Retrying resumes a failed initialization."
	huma.Register(api, huma.Operation{
		OperationID: "getIslandLobbyReplay", Path: "/game/from-lobby/{lobby_id}/replay", Method: http.MethodGet,
		Summary:     "View the Island Battles replay history for a started lobby",
		Description: "Current lobby members can view saved generations, including clash effects with survivor losses and defeated troop outlines. Older replays without clash events remain viewable.",
		Tags:        []string{"Island Battles"}, Security: []map[string][]string{{"LobbyBearer": {}}},
	}, game.Replay)
	huma.Register(api, huma.Operation{
		OperationID: "getIslandLobbyGeneration", Path: "/game/from-lobby/{lobby_id}/generation", Method: http.MethodGet,
		Summary: "Read the current generation for a roster player", Description: "Requires a current authorized lobby snapshot and frozen roster membership. Returns logical castle IDs, troop state, recorded clashes with per-participant before/after counts, and your turn completion flag.",
		Tags: []string{"Island Battles"}, Security: []map[string][]string{{"LobbyBearer": {}}},
	}, game.CurrentGeneration)
	huma.Register(api, huma.Operation{
		OperationID: "registerGenerationCommand", Path: "/generation/commands", Method: http.MethodPost,
		Summary: "Register an atomic player command batch and optionally complete the turn", Description: "Only owned send_soldier_group commands are accepted. Complete defaults to true; an empty batch can complete a turn. The last frozen roster completion advances exactly once. Retry the same request UUID and payload safely; changed payloads and stale new requests conflict.",
		Tags: []string{"Island Battles"}, Security: []map[string][]string{{"LobbyBearer": {}}},
	}, game.RegisterGenerationCommands)
}

func SetupRoutes(api huma.API, env *handlers.Env) {
	// GORM JSON marshals as raw JSON, rather than the base64 string Huma infers
	// from its underlying byte slice. Describe command parameters accordingly.
	api.OpenAPI().Components.Schemas.RegisterTypeAlias(reflect.TypeOf(datatypes.JSON{}), reflect.TypeOf(json.RawMessage{}))
	huma.Register(api, huma.Operation{Path: "/map/create", Method: http.MethodPost, DefaultStatus: http.StatusOK,
		Summary: "Create a map template with optional castle upgrades.", Description: "Castle upgrades are a map of upgrade type to level. Omitted upgrades default to level 1; speed supports levels 1 through 4, and clash_stop_generations supports levels 1 through 3."}, env.CreateMapTemplate)
	huma.Register(api, huma.Operation{Path: "/game/{id}/graph", Method: http.MethodGet, DefaultStatus: http.StatusOK,
		Summary: "Get a graph visualization of the game."}, env.GetGameGraph)
}
