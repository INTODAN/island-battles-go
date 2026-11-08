package routes

import (
	"encoding/json"
	"fmt"
	"islandBattles/backend/handlers"
	"islandBattles/backend/lobbyadapter"
	"islandBattles/backend/models"
	"islandBattles/backend/observability"
	"islandBattles/backend/services"
	"islandBattles/lobby/application"
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
// every problem response, as httpapi.BoundErrorDetails describes, after
// observability.RecordProblems has kept each 5xx problem whole for the
// request's error line.
func NewAPIConfig(title, version string) huma.Config {
	config := huma.DefaultConfig(title, version)
	config.Transformers = append(config.Transformers, observability.RecordProblems, httpapi.BoundErrorDetails)
	config.Components.Schemas = huma.NewMapRegistry("#/components/schemas/", func(t reflect.Type, hint string) string {
		return strings.TrimSuffix(huma.DefaultSchemaNamer(t, hint), "View")
	})
	return config
}

// LobbyAuthentication verifies lobby sessions and issues connection tokens.
type LobbyAuthentication interface {
	domain.Authenticator
	domain.ConnectionTokenIssuer
}

// SetupLobbyRoutes embeds reusable lobby commands and the explicit game binding.
// Centrifugo's trusted proxy endpoints are registered separately from OpenAPI.
// Lobby requests end with a lobby retention pass when one is due.
func SetupLobbyRoutes(api huma.API, lobbyApplication domain.Application, authentication LobbyAuthentication, gameBinding *application.GameBinding[models.BindingResult], retention domain.RetentionRunner, game *handlers.LobbyGame) {
	httpapi.Register(api, lobbyadapter.WithGameStart(lobbyApplication, gameBinding), authentication, authentication, retention)
	lobbyadapter.Register(api, gameBinding, authentication)
	api.OpenAPI().Paths["/lobby/v1/games/{game_key}/lobbies/{lobby_id}/start"].Post.Description += " Island Battles initializes one playable generation and waits for every frozen roster player to complete their turn. Retrying resumes a failed initialization. Each roster player keeps the bot they chose with PUT /game/lobbies/{lobby_id}/bot; a roster player without one fails the start with game_requirement of type " + services.BotNotChosenProblemType + "."
	api.OpenAPI().Paths["/lobby/v1/games/{game_key}/lobbies/{lobby_id}/ready"].Put.Description += " Island Battles requires a bot choice first, made with PUT /game/lobbies/{lobby_id}/bot, and refuses to set a member without one ready with game_requirement of type " + services.BotNotChosenProblemType + "."
	lobbyBots := &handlers.LobbyBots{Authenticator: authentication, Service: services.LobbyBots{Database: game.Database, Lobbies: lobbyApplication, GameKey: lobbyadapter.GameKey}}
	huma.Register(api, huma.Operation{
		OperationID: "chooseIslandLobbyBot", Path: "/game/lobbies/{lobby_id}/bot", Method: http.MethodPut,
		Summary: "Choose your bot for a waiting lobby",
		Description: "Requires an active membership of the Island Battles lobby, while it is waiting. Every member chooses a bot before getting ready: kind localhost is the member's own bot, which island-battles-bot serve runs on their machine and their game client relays each turn to; kind default, with a level that GET /bots/defaults lists, is a default bot that the backend plays itself. " +
			"Getting ready without a choice fails with the lobby's 409 problem game_requirement of type " + services.BotNotChosenProblemType + ". A member can change their choice while the lobby waits, also once ready. " +
			"When the lobby starts, the game copies each roster player's choice, which GenerationState.bot reports from then on, and choosing conflicts (409). A 409 stores nothing: the choice is saved only while the lobby is still waiting, so the one a start copies is always the last that was answered 200. " +
			"An invalid choice is 422, and a caller who isn't a member of the lobby gets 404. Choosing changes nothing in the lobby itself, so it publishes no lobby.changed; clients re-read the choices when the lobby changes, as it does when a member gets ready. Returns every member's choice, as GET /game/lobbies/{lobby_id}/bots does.",
		Tags: []string{"Island Battles"}, Security: []map[string][]string{{"LobbyBearer": {}}},
		// The service checks the choice itself, after authenticating the
		// caller, so a request without credentials is refused first.
		SkipValidateBody: true,
	}, lobbyBots.Choose)
	// GenerationState carries the same schema, so it describes only a bot.
	dropSchemaLink(api, reflect.TypeFor[models.BotChoiceView]())
	huma.Register(api, huma.Operation{
		OperationID: "listIslandLobbyBots", Path: "/game/lobbies/{lobby_id}/bots", Method: http.MethodGet,
		Summary: "List the bots that a lobby's members chose",
		Description: "Requires an active membership of the Island Battles lobby, in any phase. Lists every member in the lobby's member order with the bot they chose with PUT /game/lobbies/{lobby_id}/bot, which is absent until they choose one, so a lobby can show who plays with which bot. " +
			"The choices are deleted with the lobby, and a member who leaves keeps their choice for when they rejoin. A started game keeps its own copy of each roster player's choice, which GenerationState.bot reports.",
		Tags: []string{"Island Battles"}, Security: []map[string][]string{{"LobbyBearer": {}}},
	}, lobbyBots.List)
	huma.Register(api, huma.Operation{
		OperationID: "getIslandLobbyReplay", Path: "/game/from-lobby/{lobby_id}/replay", Method: http.MethodGet,
		Summary:     "View the Island Battles replay history for a started lobby",
		Description: "Players on the game's frozen roster can view every stored generation of the game, built from the generations on each request, including clash effects with survivor losses and defeated troop outlines, and the outcome once the game has ended. The game does not consult the lobby, so access continues after the lobby is deleted. The game, and with it the replay, is deleted 7 days after its last generation by default, and then this returns 404.",
		Tags:        []string{"Island Battles"}, Security: []map[string][]string{{"LobbyBearer": {}}},
	}, game.Replay)
	huma.Register(api, huma.Operation{
		OperationID: "getIslandLobbyGeneration", Path: "/game/from-lobby/{lobby_id}/generation", Method: http.MethodGet,
		Summary: "Read the current generation for a roster player", Description: "Requires a place on the game's frozen roster. The game does not consult the lobby, so access continues after the lobby is deleted. Returns castle and soldier group IDs, which are never reassigned within a game, troop state, every castle's and soldier group's upgrade levels, recorded clashes with per-participant before/after counts, your turn completion flag, the turn's deadline, and the outcome once the game has ended. " +
			"Like every game request, it first applies the turn's deadline once it has passed, which ends the game. A finished game stays readable until the game is deleted, 7 days after its last generation by default; then this returns 404.",
		Tags: []string{"Island Battles"}, Security: []map[string][]string{{"LobbyBearer": {}}},
	}, game.CurrentGeneration)
	huma.Register(api, huma.Operation{
		OperationID: "applyIslandLobbyTurnDeadline", Path: "/game/from-lobby/{lobby_id}/deadline", Method: http.MethodPost,
		Summary: "Apply the current turn's deadline for a roster player",
		Description: "Requires a place on the game's frozen roster; the lobby is not consulted. The server runs no timers, so a turn times out when a request applies its deadline, GenerationState.turn_deadline_at. Every game request does that first, and a client that is waiting for other players calls this once about a second after the deadline. " +
			"Before the deadline, and for a game that has ended or has no turn timeout, it changes nothing. After it, every team with a player who hasn't completed the turn loses, the game ends with reason turn_timeout, and among the other teams still standing the team with the most castles wins; equal counts, or no team left, are a draw. The open generation stays the final one, game.ended is pushed to every roster player, and later commands conflict (409) with problem type " + services.GameEndedProblemType + ". " +
			"Calling it again is safe and ends the game once. Returns your current state, as GET /game/from-lobby/{lobby_id}/generation does.",
		Tags: []string{"Island Battles"}, Security: []map[string][]string{{"LobbyBearer": {}}},
	}, game.ApplyTurnDeadline)
	huma.Register(api, huma.Operation{
		OperationID: "getIslandLobbyGenerationLogs", Path: "/game/from-lobby/{lobby_id}/logs", Method: http.MethodGet,
		Summary: "Read a generation's game log lines for a roster player",
		Description: "Requires a place on the game's frozen roster; the lobby is not consulted. Returns the game log lines that players' bots sent with their batches for the generation, grouped by player, each named by the display name they had at the start, never by ID. " +
			"While the game runs you see only your own team's lines; once it has an outcome you see every team's. hidden_team_colors lists the teams you can't see yet, without saying whether they logged anything. " +
			"A generation of another game returns 404. The lines are deleted with the game, 7 days after its last generation by default; then this returns 404.",
		Tags: []string{"Island Battles"}, Security: []map[string][]string{{"LobbyBearer": {}}},
	}, game.GenerationLogs)
	// The demo is streamed, so Huma can't derive its response from the
	// handler; the published file format is the Demo schema.
	demo := api.OpenAPI().Components.Schemas.Schema(reflect.TypeOf(models.Demo{}), true, "")
	// Huma can't mark a reference nullable, and a demo of a running game has
	// a null outcome.
	demoSchema := api.OpenAPI().Components.Schemas.SchemaFromRef(demo.Ref)
	outcome := demoSchema.Properties["outcome"]
	demoSchema.Properties["outcome"] = &huma.Schema{Description: outcome.Description, AnyOf: []*huma.Schema{{Ref: outcome.Ref}, {Type: "null"}}}
	demoSchema.PrecomputeMessages()
	huma.Register(api, huma.Operation{
		OperationID: "getIslandLobbyDemo", Path: "/game/from-lobby/{lobby_id}/demo", Method: http.MethodGet,
		Summary: "Save a lobby's game as a demo file for a roster player",
		Description: "Requires a place on the game's frozen roster; the lobby is not consulted. Returns the game as a demo file, the Demo schema, as an attachment named island-battles-game-<game_id>-generation-<n>.json, where n is the number of generations played. " +
			"Any roster player can save a demo at any time: one saved during the game holds the generations played so far and a null outcome. Each frame's game log lines follow the rule of GET /game/from-lobby/{lobby_id}/logs when the demo is saved: while the game runs only your own team's lines, once it has ended every team's. " +
			"The demo viewer at /game/demo-viewer/ opens the file without the lobby, a session or the game. The body is streamed, and a long game with full logs takes tens of megabytes, so it is packed with gzip (Content-Encoding: gzip) when Accept-Encoding allows it. The game is deleted 7 days after its last generation by default; then this returns 404.",
		Tags: []string{"Island Battles"}, Security: []map[string][]string{{"LobbyBearer": {}}},
		Responses: map[string]*huma.Response{"200": {
			Description: "The demo file.",
			Headers: map[string]*huma.Param{
				"Content-Disposition": {Description: "attachment, with the demo's file name.", Schema: &huma.Schema{Type: huma.TypeString}},
				"Content-Encoding":    {Description: "gzip when the request's Accept-Encoding allows it; absent otherwise.", Schema: &huma.Schema{Type: huma.TypeString}},
			},
			Content: map[string]*huma.MediaType{"application/json": {Schema: demo}},
		}},
	}, game.Demo)
	// A demo is a file format, like a map file.
	dropSchemaLink(api, reflect.TypeFor[models.Demo]())
	for _, parameter := range api.OpenAPI().Paths["/game/from-lobby/{lobby_id}/logs"].Get.Parameters {
		if parameter.Name == "generation_id" {
			// The service requires it after authorizing the caller; see
			// GenerationLogs. Huma validates requests against the schema it
			// registered, so the documented minimum goes on a copy: on the
			// original, generation_id=0 would be refused before authentication.
			parameter.Required = true
			minimum := 1.0
			documented := *parameter.Schema
			documented.Minimum = &minimum
			parameter.Schema = &documented
		}
	}
	huma.Register(api, huma.Operation{
		OperationID: "registerGenerationCommand", Path: "/generation/commands", Method: http.MethodPost,
		Summary: "Register an atomic player command batch and optionally complete the turn", Description: "Requires a place on the game's frozen roster; the lobby is not consulted. Players send send_soldier_group and upgrade_castle commands for castles their team owns; other command types are forbidden (403). " +
			"upgrade_castle buys the next level of one upgrade type and pays that level's soldier_cost, listed by GET /game/upgrades, from the castle's soldiers. A castle may be upgraded several times in one turn, and each upgrade buys the next level. " +
			"Every command is checked against the generation and every command already accepted this turn, yours and your teammates', so an accepted command cannot fail when the turn runs. An invalid command rejects the whole batch, and nothing of it is stored. " +
			"Malformed parameters are 422. Rejections that clients tell apart carry a problem type: a castle that is not in the generation is 422 " + services.CastleNotFoundProblemType + ", an unknown upgrade_type is 422 " + services.UnknownUpgradeTypeProblemType + ", a castle your team does not own is 403 " + services.CastleNotOwnedProblemType + ", " +
			"an upgrade past its type's max_level, counting this turn's upgrades, is 409 " + services.UpgradeMaxLevelProblemType + ", and soldiers or an upgrade cost beyond what the castle has left after this turn's reservations is 409 " + services.InsufficientSoldiersProblemType + ". A castle may spend all its soldiers. " +
			"Complete defaults to true; an empty batch can complete a turn. The last frozen roster completion advances exactly once. " +
			"The game ends after the turn that leaves one team with castles or soldier groups, a draw when the last teams are eliminated in the same turn, or after the turn that creates generation 300, where the team with the most castles wins. " +
			"That turn's generation is the last, its state carries the outcome, and every later command conflicts (409) with problem type " + services.GameEndedProblemType + ". " +
			"A turn also ends the game when its deadline, GenerationState.turn_deadline_at, passes before every roster player completed it: each team with a player who hadn't loses (outcome reason turn_timeout), the open generation stays the last, and a batch for it after the deadline conflicts with the same problem type. " +
			"Retry the same request UUID and payload safely, also after the game ended; changed payloads and stale new requests conflict. " +
			"A batch may also carry game log lines, which are stored in order with the generation and read with GET /game/from-lobby/{lobby_id}/logs. A player has at most 100 lines per generation, each of at most 500 characters; a batch over either limit is rejected whole (422) with problem type " + services.GameLogLimitProblemType + ". " +
			"A batch may also carry the memory of the player's bot, any JSON object of at most 64 KiB as compact JSON. The batch with complete true saves it, replacing the player's previous memory, and an absent or null memory clears it; other batches ignore it. " +
			"GenerationState.memory returns the memory sent with the player's latest completed turn, and the retry hash covers it. Larger memory is rejected whole (422) with problem type " + services.BotMemoryTooLargeProblemType + ".",
		Tags: []string{"Island Battles"}, Security: []map[string][]string{{"LobbyBearer": {}}},
		// The service validates the body itself so forbidden command types and
		// unowned castles keep their 403 status instead of a schema 422.
		SkipValidateBody: true,
	}, game.RegisterGenerationCommands)
	registry := api.OpenAPI().Components.Schemas
	// The service decodes parameters by command_type, so the published command
	// is one of the two player command shapes.
	send := registry.Schema(reflect.TypeOf(models.SendSoldierGroupCommand{}), true, "")
	upgrade := registry.Schema(reflect.TypeOf(models.UpgradeCastleCommand{}), true, "")
	*registry.Map()["PublicGenerationCommand"] = huma.Schema{
		OneOf:         []*huma.Schema{send, upgrade},
		Discriminator: &huma.Discriminator{PropertyName: "command_type", Mapping: map[string]string{string(models.SendSoldierGroup): send.Ref, string(models.UpgradeCastle): upgrade.Ref}},
	}
	// Published on each roster player's personal channel rather than over HTTP.
	registry.Schema(reflect.TypeOf(models.GenerationEvent{}), true, "")
	registry.Schema(reflect.TypeOf(models.GameEndedEvent{}), true, "")
	// The stateless bot contract: what a bot is given each turn and what it
	// answers, whoever runs it.
	botTurn := registry.SchemaFromRef(registry.Schema(reflect.TypeOf(models.BotTurn{}), true, "").Ref)
	botTurn.Description = "What a bot is given to play one turn. A bot is a stateless function: whatever it needs to keep between turns goes in the memory it returns, which the server stores per player and game."
	botTurnResult := registry.SchemaFromRef(registry.Schema(reflect.TypeOf(models.BotTurnResult{}), true, "").Ref)
	botTurnResult.Description = "A bot's answer to a BotTurn. Its commands, logs and memory are sent as one completing batch of POST /generation/commands."
	// Sent inside the generic lobby create request as its game_settings.
	settings := registry.SchemaFromRef(registry.Schema(reflect.TypeOf(models.LobbySettings{}), true, "").Ref)
	settings.Description = "Island Battles game_settings for a lobby create request. Choose at most one map: map_template_id or map. The map must be made for exactly the lobby's teams: player_amount equal to the team count, and castle team colours exactly 1 to that count, in team order, with 0 for neutral castles. Without a map the game starts on a generated map. turn_timeout_seconds sets how long each turn lasts, from 5 to 600 seconds, 30 when left out, or null for no limit. An invalid choice fails lobby creation with invalid_game_settings, and its detail names each invalid field, such as game_settings.map.castles[0].x."
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
			"Castle upgrades are a map of upgrade type to level. Omitted upgrades default to level 1; " + upgradeLevelRanges() + ". " +
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
	huma.Register(api, huma.Operation{OperationID: "listUpgrades", Path: "/game/upgrades", Method: http.MethodGet,
		Summary: "List the castle upgrades, their levels and their costs.",
		Description: "Lists every upgrade type, sorted by type, with what its levels measure and, for each level from 1 to max_level, its effect and the soldiers a castle pays to reach it from the level below. Every castle starts at level 1. " +
			"The table is static and needs no authentication. Castles and soldier groups carry their current levels, and upgrade_castle in POST /generation/commands buys the next one."}, handlers.ListUpgrades)
	huma.Register(api, huma.Operation{OperationID: "listDefaultBots", Path: "/bots/defaults", Method: http.MethodGet,
		Summary: "List the default bots that the backend plays.",
		Description: "Lists the default bots in level order, each with its level, a name and a description of how it plays. Higher levels play better. " +
			"The backend plays a default bot itself, with no client: it plays each turn as soon as the generation is created, so it never misses a turn's deadline. " +
			"A game in which only default bots are left plays on by itself, and each game request continues it for a few seconds. The list is static and needs no authentication."}, handlers.ListDefaultBots)
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

// upgradeLevelRanges says which levels each upgrade type supports, from the
// upgrade table, such as "speed supports levels 1 through 4".
func upgradeLevelRanges() string {
	ranges := []string{}
	for _, upgrade := range models.UpgradeTable() {
		ranges = append(ranges, fmt.Sprintf("%s supports levels 1 through %d", upgrade.Type, upgrade.MaxLevel))
	}
	if len(ranges) < 2 {
		return strings.Join(ranges, "")
	}
	return strings.Join(ranges[:len(ranges)-1], ", ") + ", and " + ranges[len(ranges)-1]
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
