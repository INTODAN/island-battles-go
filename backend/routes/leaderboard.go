package routes

import (
	"net/http"
	"reflect"

	"islandBattles/backend/handlers"
	"islandBattles/backend/models"

	"github.com/danielgtaylor/huma/v2"
)

// SetupLeaderboardRoutes registers the public leaderboard, which needs no
// credential, and the admin's routes that take a name off it, which take the
// admin API key, adminAPIKey; an empty key refuses every admin request.
func SetupLeaderboardRoutes(api huma.API, handler *handlers.Leaderboard, adminAPIKey string) {
	const listed = "Public: it needs no credential. It lists the competitors that have played at least one rated game: each registered bot name, shown with its owner's display name (\"Kraken, by Dana\"), each account's hand play, shown under the account's display name, and the house bots, one per default bot level, shown as Bot (level N). " +
		"A deleted bot name and an account that the admin hid aren't listed, and the game holds no Google name, picture or email to show. Ratings are rounded to whole numbers. " +
		"The answer carries Cache-Control: " + handlers.LeaderboardCacheControl + ", and CloudFront keeps it for a minute, so a change, such as a game that ends, shows within a minute. "
	huma.Register(api, huma.Operation{
		OperationID: "listIslandLeaderboard", Path: "/leaderboard", Method: http.MethodGet,
		Summary: "Read the leaderboard: the competitors ranked by rating",
		Description: listed + "The order is rating, highest first, then more rated games first, then the older competitor. rank is the place in that order within the kind asked for, from 1, and equal ratings, as shown, get the same rank. " +
			"kind lists only one kind, ranked among itself; limit (1 to 100, default 50) and offset (default 0) page through it, and total counts the competitors of that kind.",
		Tags: []string{"Island Battles"},
	}, handler.List)
	huma.Register(api, huma.Operation{
		OperationID: "getIslandCompetitor", Path: "/leaderboard/{competitor_id}", Method: http.MethodGet,
		Summary: "Read a competitor's page: its place on the leaderboard and its latest results",
		Description: listed + "Returns the competitor's leaderboard entry, ranked among every kind, and history, its latest 50 rated results, newest first, each with its result, its rating before and after, and the game's other competitors by the name they are shown by now. " +
			"An opponent that was deleted since, or whose account is hidden, is shown as Deleted bot or Deleted player, without its ID or owner. A competitor that the leaderboard doesn't list is 404: unknown, deleted, hidden, or without a rated game yet.",
		Tags:   []string{"Island Battles"},
		Errors: []int{http.StatusNotFound},
	}, handler.Competitor)

	huma.Register(api, huma.Operation{
		OperationID: "listIslandRegisteredBots", Path: "/bots/registered", Method: http.MethodGet,
		Summary: "Search the registered bots: every bot name with a URL, rated or not",
		Description: "Public: it needs no credential. It lists every registered bot name that has a hosted bot URL, across all accounts, with its owner's display name and its URL, whether or not it has played a rated game. " +
			"Anyone may choose one by its competitor_id: for their own team, as a member's {\"kind\": \"hosted\", \"competitor_id\": \"…\"}, or as a seat, {\"bot_competitor_id\": \"…\"}, and the server plays it at its URL, without its owner joining. " +
			"A deleted bot name, one without a URL, and the bots of an account that the admin hid aren't listed. " +
			"search keeps only the bots whose name or owner's display name holds it, without regard to case. The order is rated bots first, by rating, highest first, then unrated bots by name; limit (1 to 100, default 50) and offset (default 0) page through it, and total counts the bots that match.",
		Tags: []string{"Island Battles"},
	}, handler.RegisteredBots)

	registerAdminAPIKeyScheme(api)
	const admin = "Admins only: send the deployment's admin API key in the " + AdminAPIKeyHeader + " header. A missing or wrong key returns 401, and when the deployment sets no key every request returns 403. " +
		"The leaderboard's answers are cached for a minute, so the change shows within a minute. "
	adminOnly := huma.Middlewares{requireAdminAPIKey(api, adminAPIKey)}
	huma.Register(api, huma.Operation{
		OperationID: "removeIslandBotName", Path: "/admin/competitors/{competitor_id}", Method: http.MethodDelete, DefaultStatus: http.StatusNoContent,
		Summary: "Remove an offensive bot name",
		Description: admin + "Deletes a bot name as its owner could, with its rating: it leaves the leaderboard, its page is 404, and other competitors' history shows it as Deleted bot. The name is free at once, and the owner can register another. " +
			"An account's hand play or a house bot is 422: hide the account of an offensive display name instead. An unknown or already deleted competitor is 404.",
		Tags: []string{"Island Battles"}, Security: adminAPIKeySecurity, Middlewares: adminOnly,
		Errors: []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound, http.StatusUnprocessableEntity},
	}, handler.RemoveBotName)
	huma.Register(api, huma.Operation{
		OperationID: "setIslandAccountHidden", Path: "/admin/accounts/{account_id}/hidden", Method: http.MethodPut, DefaultStatus: http.StatusNoContent,
		Summary: "Hide an account from the leaderboard, or show it again",
		Description: admin + "{\"hidden\": true} hides the account's hand play and every bot name of it from the leaderboard and from other competitors' history, for an offensive display name; {\"hidden\": false} shows them again. " +
			"Nothing is deleted: the account keeps its bot names and ratings, and can still play, rated too. An account that the game doesn't know is 404.",
		Tags: []string{"Island Battles"}, Security: adminAPIKeySecurity, Middlewares: adminOnly,
		Errors: []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound},
	}, handler.SetAccountHidden)
	// A request body, not a linked resource.
	dropSchemaLink(api, reflect.TypeFor[models.SetAccountHiddenRequest]())
}
