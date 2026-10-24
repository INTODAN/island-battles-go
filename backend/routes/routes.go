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
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"gorm.io/datatypes"
)

// NewAPIConfig names API view types without their View suffix, so generated
// clients receive domain names such as Castle and Generation.
func NewAPIConfig(title, version string) huma.Config {
	config := huma.DefaultConfig(title, version)
	config.Components.Schemas = huma.NewMapRegistry("#/components/schemas/", func(t reflect.Type, hint string) string {
		return strings.TrimSuffix(huma.DefaultSchemaNamer(t, hint), "View")
	})
	return config
}

// SetupLobbyRoutes embeds reusable lobby commands and the explicit game binding.
// Centrifugo's trusted proxy endpoints are registered separately from OpenAPI.
func SetupLobbyRoutes(api huma.API, lobbyApplication domain.Application, authentication *auth.Service, gameBinding *application.GameBinding[models.BindingResult], game *handlers.LobbyGame) {
	httpapi.Register(api, lobbyadapter.WithGameStart(lobbyApplication, gameBinding), authentication, authentication)
	lobbyadapter.Register(api, gameBinding, authentication)
	api.OpenAPI().Paths["/lobby/v1/games/{game_key}/lobbies/{lobby_id}/start"].Post.Description += " Island Battles initializes one playable generation and waits for every frozen roster player to complete their turn. Retrying resumes a failed initialization."
	huma.Register(api, huma.Operation{
		OperationID: "getIslandLobbyReplay", Path: "/game/from-lobby/{lobby_id}/replay", Method: http.MethodGet,
		Summary:     "View the Island Battles replay history for a started lobby",
		Description: "Players on the game's frozen roster can view every stored generation of the game, built from the generations on each request, including clash effects with survivor losses and defeated troop outlines. The game does not consult the lobby, so access continues after the lobby is deleted.",
		Tags:        []string{"Island Battles"}, Security: []map[string][]string{{"LobbyBearer": {}}},
	}, game.Replay)
	huma.Register(api, huma.Operation{
		OperationID: "getIslandLobbyGeneration", Path: "/game/from-lobby/{lobby_id}/generation", Method: http.MethodGet,
		Summary: "Read the current generation for a roster player", Description: "Requires a place on the game's frozen roster. The game does not consult the lobby, so access continues after the lobby is deleted. Returns castle and soldier group IDs, which are never reassigned within a game, troop state, recorded clashes with per-participant before/after counts, and your turn completion flag.",
		Tags: []string{"Island Battles"}, Security: []map[string][]string{{"LobbyBearer": {}}},
	}, game.CurrentGeneration)
	huma.Register(api, huma.Operation{
		OperationID: "registerGenerationCommand", Path: "/generation/commands", Method: http.MethodPost,
		Summary: "Register an atomic player command batch and optionally complete the turn", Description: "Requires a place on the game's frozen roster; the lobby is not consulted. Only owned send_soldier_group commands are accepted; other command types are forbidden (403). Complete defaults to true; an empty batch can complete a turn. The last frozen roster completion advances exactly once. A game stops at 300 generations, and commands for its last generation conflict. Retry the same request UUID and payload safely; changed payloads and stale new requests conflict.",
		Tags: []string{"Island Battles"}, Security: []map[string][]string{{"LobbyBearer": {}}},
		// The service validates the body itself so forbidden command types and
		// unowned castles keep their 403 status instead of a schema 422.
		SkipValidateBody: true,
	}, game.RegisterGenerationCommands)
	registry := api.OpenAPI().Components.Schemas
	command := registry.Map()["PublicGenerationCommand"]
	command.Properties["command_type"].Enum = []any{string(models.SendSoldierGroup)}
	command.Properties["parameters"] = registry.Schema(reflect.TypeOf(models.SendSoldierGroupParameters{}), true, "")
	// Published on each roster player's personal channel rather than over HTTP.
	registry.Schema(reflect.TypeOf(models.GenerationEvent{}), true, "")
}

func SetupRoutes(api huma.API, env *handlers.Env) {
	// GORM JSON marshals as raw JSON, rather than the base64 string Huma infers
	// from its underlying byte slice. Describe command parameters accordingly.
	api.OpenAPI().Components.Schemas.RegisterTypeAlias(reflect.TypeOf(datatypes.JSON{}), reflect.TypeOf(json.RawMessage{}))
	huma.Register(api, huma.Operation{Path: "/map/create", Method: http.MethodPost, DefaultStatus: http.StatusOK,
		Summary: "Create a default map from a map file.",
		Description: "The body is the map file format, the MapTemplate schema. Positions are measured from the centre of the plane, so a castle's x lies from -plane_size_x/2 to plane_size_x/2 and its y from -plane_size_y/2 to plane_size_y/2. " +
			"Castle team colours run from 1 to player_amount, and 0 is neutral. A castle without starting_soldier_amount starts with neutral_starting_soldier_amount. " +
			"Castle upgrades are a map of upgrade type to level. Omitted upgrades default to level 1; speed supports levels 1 through 4, and clash_stop_generations supports levels 1 through 3. " +
			"An invalid map returns 422 with an error for each invalid field and is not stored. A map_name that another map uses returns 409."}, env.CreateMapTemplate)
	dropSchemaLink(api, reflect.TypeFor[models.MapTemplateView]())
	huma.Register(api, huma.Operation{Path: "/game/{id}/graph", Method: http.MethodGet, DefaultStatus: http.StatusOK,
		Summary: "Get a graph visualization of the game."}, env.GetGameGraph)
}

// dropSchemaLink removes the read-only $schema property that Huma's schema
// link transformer adds to every request body schema. A map file is a file
// format rather than a linked API resource, so its schema lists only the
// file's own keys, and a body that sends $schema is rejected like any other
// unknown key.
func dropSchemaLink(api huma.API, bodyType reflect.Type) {
	registry := api.OpenAPI().Components.Schemas
	schema := registry.SchemaFromRef(registry.Schema(bodyType, true, "").Ref)
	delete(schema.Properties, "$schema")
	schema.PrecomputeMessages()
}
