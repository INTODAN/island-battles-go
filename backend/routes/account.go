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
			"Returns the account's ID and display name, max_bot_names (5), its hand play and its bot names, oldest first, each bot name with its hosted bot's url, which is null for a name registered before names had URLs.",
		Tags: []string{"Island Battles"}, Security: playerTokenSecurity,
	}, handler.Read)
	huma.Register(api, huma.Operation{
		OperationID: "registerIslandBotName", Path: "/account/bot-names", Method: http.MethodPost, DefaultStatus: http.StatusCreated,
		Summary: "Register a bot name in your account, with its hosted bot's URL",
		Description: signedIn + "Registers a bot name, a competitor at a rating of 1500, with the URL of the hosted bot that plays it, and returns it. A name is 3 to 24 characters after trimming, of letters, digits, spaces, - and _, and can't start with Bot (level, otherwise 422. " +
			"The url is required and must pass a hosted bot's check (https:// on port 443, without credentials, on a host name whose addresses are all public), otherwise 422. In a rated lobby the name plays as a hosted bot at this URL. Once the name has played a rated game, the leaderboard lists it with its URL, so that others can play against it. " +
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
		OperationID: "setIslandBotNameURL", Path: "/account/bot-names/{competitor_id}", Method: http.MethodPut,
		Summary: "Change the hosted bot URL of one of your bot names",
		Description: signedIn + "Sets the bot name's url, which must pass a hosted bot's check, otherwise 422, and returns the bot name. It keeps its rating. A rated game plays the URL that the name had when the game started. A listed name's new URL shows on the leaderboard within a minute. " +
			"A name registered before names had URLs needs one before it can play in a rated lobby. Another account's bot name, or an unknown one, is 404.",
		Tags: []string{"Island Battles"}, Security: playerTokenSecurity,
		// The service checks the URL itself, after authenticating the
		// caller, so a request without credentials is refused first.
		SkipValidateBody: true,
	}, handler.SetBotNameURL)
	dropSchemaLink(api, reflect.TypeFor[models.SetBotNameURLRequest]())
	huma.Register(api, huma.Operation{
		OperationID: "deleteIslandBotName", Path: "/account/bot-names/{competitor_id}", Method: http.MethodDelete, DefaultStatus: http.StatusNoContent,
		Summary:     "Delete one of your bot names",
		Description: signedIn + "Deletes the bot name with its rating. The name is free at once, and registering it again makes a new competitor at 1500. Another account's bot name, or an unknown one, is 404.",
		Tags:        []string{"Island Battles"}, Security: playerTokenSecurity,
	}, handler.DeleteBotName)
	huma.Register(api, huma.Operation{
		OperationID: "resumeIslandBotNameAway", Path: "/account/bot-names/{competitor_id}/away-pause", Method: http.MethodDelete,
		Summary: "Let one of your bot names play away again after it missed a turn",
		Description: signedIn + "A bot name plays away when it sits in a bot seat, or when a member of another account chose it, in a rated lobby that someone else opened. One that misses a turn there loses the game, as any rated game, and is paused: it plays away in no rated game until its owner resumes it here. Your own games with it are unaffected. " +
			"Clears the bot name's away_paused and answers 200 with the bot name; resuming a name that isn't paused changes nothing. Another account's bot name, your hand play, or an unknown one is 404.",
		Tags: []string{"Island Battles"}, Security: playerTokenSecurity,
	}, handler.ResumeAway)
	huma.Register(api, huma.Operation{
		OperationID: "deleteIslandAccount", Path: "/account", Method: http.MethodDelete, DefaultStatus: http.StatusNoContent,
		Summary: "Delete your account's part of the game",
		Description: "Requires the player token of a signed-in player; a guest's token is 403 with problem type " + services.SignInRequiredProblemType + ". " +
			"Deletes every competitor of the account, its bot names and its hand play with their ratings, and the game's record of the account, unless the account is hidden from the leaderboard: then the game keeps its record, with its display name, so that it stays hidden, with its new hand play and bot names, when it plays again. Call it before the lobby's DELETE /lobby/v1/account, so the game's part is gone first. Deleting an account the game doesn't know is 204 too.",
		Tags: []string{"Island Battles"}, Security: playerTokenSecurity,
	}, handler.Delete)
}
