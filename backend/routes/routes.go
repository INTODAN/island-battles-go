package routes

import (
	"encoding/json"
	"islandBattles/backend/handlers"
	"islandBattles/backend/lobbyadapter"
	"islandBattles/backend/models"
	"islandBattles/backend/services"
	"islandBattles/lobby/application"
	"islandBattles/lobby/auth"
	"islandBattles/lobby/domain"
	"islandBattles/lobby/httpapi"
	"net/http"
	"reflect"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"gorm.io/datatypes"
)

// NewAPIConfig names API view types without their View suffix, so generated
// clients receive domain names such as Castle and Generation. It also bounds
// every problem response, as httpapi.BoundErrorDetails describes.
func NewAPIConfig(title, version string) huma.Config {
	config := huma.DefaultConfig(title, version)
	config.Transformers = append(config.Transformers, httpapi.BoundErrorDetails)
	config.Components.Schemas = huma.NewMapRegistry("#/components/schemas/", func(t reflect.Type, hint string) string {
		return strings.TrimSuffix(huma.DefaultSchemaNamer(t, hint), "View")
	})
	return config
}

// SetupLobbyRoutes embeds reusable lobby commands and the explicit game binding.
// Centrifugo's trusted proxy endpoints are registered separately from OpenAPI.
// Lobby requests end with a lobby retention pass when one is due.
func SetupLobbyRoutes(api huma.API, lobbyApplication domain.Application, authentication *auth.Service, gameBinding *application.GameBinding[models.BindingResult], retention domain.RetentionRunner, game *handlers.LobbyGame) {
	httpapi.Register(api, lobbyadapter.WithGameStart(lobbyApplication, gameBinding), authentication, authentication, retention)
	lobbyadapter.Register(api, gameBinding, authentication)
	api.OpenAPI().Paths["/lobby/v1/games/{game_key}/lobbies/{lobby_id}/start"].Post.Description += " Island Battles initializes one playable generation and waits for every frozen roster player to complete their turn. Retrying resumes a failed initialization."
	huma.Register(api, huma.Operation{
		OperationID: "getIslandLobbyReplay", Path: "/game/from-lobby/{lobby_id}/replay", Method: http.MethodGet,
		Summary:     "View the Island Battles replay history for a started lobby",
		Description: "Players on the game's frozen roster can view every stored generation of the game, built from the generations on each request, including clash effects with survivor losses and defeated troop outlines, and the outcome once the game has ended. The game does not consult the lobby, so access continues after the lobby is deleted.",
		Tags:        []string{"Island Battles"}, Security: []map[string][]string{{"LobbyBearer": {}}},
	}, game.Replay)
	huma.Register(api, huma.Operation{
		OperationID: "getIslandLobbyGeneration", Path: "/game/from-lobby/{lobby_id}/generation", Method: http.MethodGet,
		Summary: "Read the current generation for a roster player", Description: "Requires a place on the game's frozen roster. The game does not consult the lobby, so access continues after the lobby is deleted. Returns castle and soldier group IDs, which are never reassigned within a game, troop state, recorded clashes with per-participant before/after counts, your turn completion flag, and the outcome once the game has ended. A finished game stays readable.",
		Tags: []string{"Island Battles"}, Security: []map[string][]string{{"LobbyBearer": {}}},
	}, game.CurrentGeneration)
	huma.Register(api, huma.Operation{
		OperationID: "registerGenerationCommand", Path: "/generation/commands", Method: http.MethodPost,
		Summary: "Register an atomic player command batch and optionally complete the turn", Description: "Requires a place on the game's frozen roster; the lobby is not consulted. Only owned send_soldier_group commands are accepted; other command types are forbidden (403). Complete defaults to true; an empty batch can complete a turn. The last frozen roster completion advances exactly once. " +
			"The game ends after the turn that leaves one team with castles or soldier groups, a draw when the last teams are eliminated in the same turn, or after the turn that creates generation 300, where the team with the most castles wins. " +
			"That turn's generation is the last, its state carries the outcome, and every later command conflicts (409) with problem type " + services.GameEndedProblemType + ". " +
			"Retry the same request UUID and payload safely, also after the game ended; changed payloads and stale new requests conflict.",
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
	registry.Schema(reflect.TypeOf(models.GameEndedEvent{}), true, "")
	// Sent inside the generic lobby create request as its game_settings.
	settings := registry.SchemaFromRef(registry.Schema(reflect.TypeOf(models.LobbySettings{}), true, "").Ref)
	settings.Description = "Island Battles game_settings for a lobby create request. Choose at most one map: map_template_id or map. The map must be made for exactly the lobby's teams: player_amount equal to the team count, and castle team colours exactly 1 to that count, in team order, with 0 for neutral castles. Without a map the game starts on a generated map. An invalid choice fails lobby creation with invalid_game_settings, and its detail names each invalid field, such as game_settings.map.castles[0].x."
	api.OpenAPI().Paths["/lobby/v1/lobbies"].Post.Description += " Island Battles reads game_settings as the LobbySettings schema."
}

// SetupRoutes registers the game routes every host serves. adminAPIKey
// authorizes POST /map/create; an empty key refuses every request.
func SetupRoutes(api huma.API, env *handlers.Env, adminAPIKey string) {
	// GORM JSON marshals as raw JSON, rather than the base64 string Huma infers
	// from its underlying byte slice. Describe command parameters accordingly.
	api.OpenAPI().Components.Schemas.RegisterTypeAlias(reflect.TypeOf(datatypes.JSON{}), reflect.TypeOf(json.RawMessage{}))
	registerAdminAPIKeyScheme(api)
	huma.Register(api, huma.Operation{Path: "/map/create", Method: http.MethodPost, DefaultStatus: http.StatusOK,
		Summary:  "Create a default map from a map file.",
		Security: adminAPIKeySecurity, Middlewares: huma.Middlewares{requireAdminAPIKey(api, adminAPIKey)},
		Errors: []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusConflict},
		Description: "The body is the map file format, the MapTemplate schema. Positions are measured from the centre of the plane, so a castle's x lies from -plane_size_x/2 to plane_size_x/2 and its y from -plane_size_y/2 to plane_size_y/2. " +
			"Castle team colours run from 1 to player_amount, and 0 is neutral. A castle without starting_soldier_amount starts with neutral_starting_soldier_amount. " +
			"Castle upgrades are a map of upgrade type to level. Omitted upgrades default to level 1; speed supports levels 1 through 4, and clash_stop_generations supports levels 1 through 3. " +
			"An invalid map returns 422 with an error for each invalid field and is not stored. A map_name that another map uses returns 409. " +
			"Admins only: send the deployment's admin API key in the " + AdminAPIKeyHeader + " header. A missing or wrong key returns 401, and when the deployment sets no key every request returns 403."}, env.CreateMapTemplate)
	dropSchemaLink(api, reflect.TypeFor[models.MapTemplateView]())

	huma.Register(api, huma.Operation{Path: "/map/validate", Method: http.MethodPost, DefaultStatus: http.StatusOK,
		Summary: "Check a map file without storing it.",
		Description: "The body is a map file, the MapTemplate schema, checked exactly as a map file is read: unknown keys, missing fields and wrong types are problems, then the same rules as /map/create apply. " +
			"Nothing is stored, and the map_name may be one that a default map already uses. " +
			"Any non-empty body, even malformed JSON, returns 200: valid is true for a map that passes, and otherwise errors lists the problems, located relative to the file, such as castles[2].x. Past 100 problems, a last entry without a location counts the rest. The map builder at /map/builder/ uses it while editing.",
		// The handler decodes the raw body itself, so Huma neither parses nor
		// rejects it and every problem comes back in the response.
		SkipValidateBody: true, MaxBodyBytes: 1 << 20, BodyReadTimeout: 5 * time.Second}, handlers.ValidateMapTemplate)
	api.OpenAPI().Paths["/map/validate"].Post.RequestBody.Content["application/json"].Schema = &huma.Schema{Ref: "#/components/schemas/MapTemplate"}
	huma.Register(api, huma.Operation{OperationID: "listMapTemplates", Path: "/map/templates", Method: http.MethodGet,
		Summary: "List the default maps a lobby can start on.",
		Description: "Lists every default map whose castle team colours are exactly 1 to player_amount, ordered by name. " +
			"Choose one when creating an Island Battles lobby by passing its map_template_id in game_settings, with exactly team_count teams. " +
			"Maps that fit no lobby are left out: those whose team colours skip a colour, and those made for fewer than 2 or more than 16 teams."}, env.ListMapTemplates)
}

// SetupGameGraphRoute registers the game graph page, which reads any game
// without authentication. Only the local server host serves it, never the
// public Lambda host.
func SetupGameGraphRoute(api huma.API, env *handlers.Env) {
	huma.Register(api, huma.Operation{Path: "/game/{id}/graph", Method: http.MethodGet, DefaultStatus: http.StatusOK,
		Summary:     "Get a graph visualization of the game.",
		Description: "A development page that reads any game without authentication. Only the local server host serves it, never the public Lambda host."}, env.GetGameGraph)
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
