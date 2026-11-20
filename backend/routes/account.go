package routes

import (
	"net/http"
	"reflect"

	"islandBattles/backend/handlers"
	"islandBattles/backend/models"
	"islandBattles/backend/services"

	"github.com/danielgtaylor/huma/v2"
)

// SetupAccountRoutes registers a signed-in player's account: their hand play
// and the bot names they register, each a competitor that later gets a
// rating. They take the player tokens that SetupGameRoutes describes.
func SetupAccountRoutes(api huma.API, handler *handlers.Accounts) {
	const signedIn = "Requires the player token of a signed-in player, which names their account; a guest's token is 403 with problem type " + services.SignInRequiredProblemType + ". " +
		"Every call stores the account the first time the game sees it, with its hand play, and the display name of the token whenever it changed, so a rename in the lobby reaches the game with the next player token. "
	huma.Register(api, huma.Operation{
		OperationID: "getIslandAccount", Path: "/account", Method: http.MethodGet,
		Summary: "Read your account: your hand play and your bot names",
		Description: signedIn + "A competitor is what gets a rating: each bot name an account registers, and the account's hand play, shown under its display name. Each starts at a rating of 1500. " +
			"Returns the account's ID and display name, max_bot_names (5), its hand play and its bot names, oldest first.",
		Tags: []string{"Island Battles"}, Security: playerTokenSecurity,
	}, handler.Read)
	huma.Register(api, huma.Operation{
		OperationID: "registerIslandBotName", Path: "/account/bot-names", Method: http.MethodPost, DefaultStatus: http.StatusCreated,
		Summary: "Register a bot name in your account",
		Description: signedIn + "Registers a bot name, a competitor at a rating of 1500, and returns it. A name is 3 to 24 characters after trimming, of letters, digits, spaces, - and _, and can't start with Bot (level, otherwise 422. " +
			"Names are unique across all accounts without regard to case: a taken name is 409 with problem type " + services.BotNameTakenProblemType + ". An account has at most 5 bot names: a sixth is 409 with problem type " + services.BotNameLimitProblemType + ". " +
			"A name can't be renamed: delete it and register another.",
		Tags: []string{"Island Battles"}, Security: playerTokenSecurity,
		// The service checks the name itself, after authenticating the
		// caller, so a request without credentials is refused first.
		SkipValidateBody: true,
	}, handler.RegisterBotName)
	// A request body, not a linked resource.
	dropSchemaLink(api, reflect.TypeFor[models.RegisterBotNameRequest]())
	huma.Register(api, huma.Operation{
		OperationID: "deleteIslandBotName", Path: "/account/bot-names/{competitor_id}", Method: http.MethodDelete, DefaultStatus: http.StatusNoContent,
		Summary:     "Delete one of your bot names",
		Description: signedIn + "Deletes the bot name with its rating. The name is free at once, and registering it again makes a new competitor at 1500. Another account's bot name, or an unknown one, is 404.",
		Tags:        []string{"Island Battles"}, Security: playerTokenSecurity,
	}, handler.DeleteBotName)
	huma.Register(api, huma.Operation{
		OperationID: "deleteIslandAccount", Path: "/account", Method: http.MethodDelete, DefaultStatus: http.StatusNoContent,
		Summary: "Delete your account's part of the game",
		Description: "Requires the player token of a signed-in player; a guest's token is 403 with problem type " + services.SignInRequiredProblemType + ". " +
			"Deletes every competitor of the account, its bot names and its hand play with their ratings, and the game's record of the account. Call it before the lobby's DELETE /lobby/v1/account, so the game's part is gone first. Deleting an account the game doesn't know is 204 too.",
		Tags: []string{"Island Battles"}, Security: playerTokenSecurity,
	}, handler.Delete)
}
