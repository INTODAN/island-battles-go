package handlers_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2/humatest"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"islandBattles/backend/dal"
	"islandBattles/backend/handlers"
	"islandBattles/backend/internal/testutils"
	"islandBattles/backend/models"
	"islandBattles/backend/routes"
	"islandBattles/backend/services"
)

// leaderboardAPI serves the account routes, to the players that players
// names by their whole Authorization header, and the leaderboard's routes,
// whose admin routes take adminAPIKey.
func leaderboardAPI(t *testing.T, database *gorm.DB, players bearerPlayers, adminAPIKey string) humatest.TestAPI {
	t.Helper()
	_, api := humatest.New(t, routes.NewAPIConfig("test", "1.0.0"))
	routes.SetupAccountRoutes(api, &handlers.Accounts{Players: players, Service: services.Accounts{Database: database}})
	routes.SetupLeaderboardRoutes(api, &handlers.Leaderboard{Service: services.Leaderboard{Database: database}}, adminAPIKey)
	return api
}

// played gives a competitor the record that rated games would have left:
// its rating and games, all won, the latest just now.
func played(t *testing.T, database *gorm.DB, competitorID string, rating float64, games int) {
	t.Helper()
	now := time.Now()
	if err := database.Model(&models.Competitor{ID: competitorID}).Select("Rating", "Games", "Wins", "LastPlayedAt").
		Updates(&models.Competitor{Rating: rating, Games: games, Wins: games, LastPlayedAt: &now}).Error; err != nil {
		t.Fatal(err)
	}
}

func houseBot(t *testing.T, database *gorm.DB, level uint) models.Competitor {
	t.Helper()
	house, err := dal.FindOrCreateHouseCompetitor(database, level)
	if err != nil {
		t.Fatal(err)
	}
	return house
}

// handOf returns the account's hand play of the player with token, which
// GET /account creates.
func handOf(t *testing.T, api humatest.TestAPI, token string) models.CompetitorView {
	t.Helper()
	var account models.IslandAccountView
	expectStatus(t, api.Get("/account", "Authorization: Bearer "+token), http.StatusOK, &account)
	return account.Hand
}

func registerBotName(t *testing.T, api humatest.TestAPI, token, name string) models.CompetitorView {
	t.Helper()
	var competitor models.CompetitorView
	expectStatus(t, api.Post("/account/bot-names", "Authorization: Bearer "+token, map[string]string{"name": name}), http.StatusCreated, &competitor)
	return competitor
}

func readLeaderboard(t *testing.T, api humatest.TestAPI, query string) models.LeaderboardView {
	t.Helper()
	var leaderboard models.LeaderboardView
	expectStatus(t, api.Get("/leaderboard"+query), http.StatusOK, &leaderboard)
	return leaderboard
}

// entryNames shows a page's entries as rank, name and owner.
func entryNames(entries []models.LeaderboardEntryView) string {
	shown := []string{}
	for _, entry := range entries {
		name := fmt.Sprintf("%d %s", entry.Rank, entry.Name)
		if entry.Owner != nil {
			name += ", by " + *entry.Owner
		}
		shown = append(shown, name)
	}
	return fmt.Sprint(shown)
}

func TestTheLeaderboardListsACompetitorAfterItsFirstRatedGame(t *testing.T) {
	database := testutils.InitTestDB(t)
	api := leaderboardAPI(t, database, bearerPlayers{"Bearer dana": signedInPlayer("Dana")}, testAdminAPIKey)
	kraken := registerBotName(t, api, "dana", "Kraken")
	houseBot(t, database, 2)
	if leaderboard := readLeaderboard(t, api, ""); leaderboard.Total != 0 || len(leaderboard.Entries) != 0 {
		t.Fatalf("leaderboard = %+v, want nobody before a rated game", leaderboard)
	}
	expectStatus(t, api.Get("/leaderboard/"+kraken.ID), http.StatusNotFound, nil)

	played(t, database, kraken.ID, 1520, 1)
	leaderboard := readLeaderboard(t, api, "")
	if leaderboard.Total != 1 || len(leaderboard.Entries) != 1 {
		t.Fatalf("leaderboard = %+v, want Kraken alone", leaderboard)
	}
	entry := leaderboard.Entries[0]
	if entry.CompetitorID != kraken.ID || entry.Rank != 1 || entry.Kind != models.BotCompetitor || entry.Name != "Kraken" || entry.Owner == nil || *entry.Owner != "Dana" ||
		entry.Rating != 1520 || entry.Games != 1 || entry.Wins != 1 || entry.LastPlayedAt.IsZero() {
		t.Fatalf("Kraken's entry = %+v", entry)
	}
}

// Equal ratings, as shown, get the same rank, and the next skips a place;
// kind ranks the competitors of one kind among themselves.
func TestTheLeaderboardRanksEqualRatingsAlike(t *testing.T) {
	database := testutils.InitTestDB(t)
	api := leaderboardAPI(t, database, bearerPlayers{"Bearer dana": signedInPlayer("Dana"), "Bearer eli": signedInPlayer("Eli")}, testAdminAPIKey)
	dana := handOf(t, api, "dana")
	kraken := registerBotName(t, api, "eli", "Kraken")
	level1, level3, level2 := houseBot(t, database, 1), houseBot(t, database, 3), houseBot(t, database, 2)
	played(t, database, dana.ID, 1520, 2)
	played(t, database, level1.ID, 1500, 3)
	// Shown as 1500 too, so ranked with level 1, after it.
	played(t, database, kraken.ID, 1499.6, 1)
	// Equal ratings and games: the older competitor first.
	played(t, database, level3.ID, 1480, 1)
	played(t, database, level2.ID, 1480, 1)

	leaderboard := readLeaderboard(t, api, "")
	if got := entryNames(leaderboard.Entries); leaderboard.Total != 5 || got != "[1 Dana 2 Bot (level 1) 2 Kraken, by Eli 4 Bot (level 3) 4 Bot (level 2)]" {
		t.Fatalf("leaderboard = %d: %s", leaderboard.Total, got)
	}
	if ratings := fmt.Sprint(leaderboard.Entries[0].Rating, leaderboard.Entries[1].Rating, leaderboard.Entries[2].Rating); ratings != "1520 1500 1500" {
		t.Fatalf("ratings = %s, want 1520 1500 1500", ratings)
	}
	// A page that starts within a tie keeps its rank.
	if page := readLeaderboard(t, api, "?limit=2&offset=2"); page.Total != 5 || entryNames(page.Entries) != "[2 Kraken, by Eli 4 Bot (level 3)]" {
		t.Fatalf("page = %d: %s", page.Total, entryNames(page.Entries))
	}
	if page := readLeaderboard(t, api, "?offset=5"); page.Total != 5 || len(page.Entries) != 0 {
		t.Fatalf("page past the end = %+v", page)
	}

	house := readLeaderboard(t, api, "?kind=house")
	if got := entryNames(house.Entries); house.Total != 3 || got != "[1 Bot (level 1) 2 Bot (level 3) 2 Bot (level 2)]" {
		t.Fatalf("house bots = %d: %s", house.Total, got)
	}
	if bots := readLeaderboard(t, api, "?kind=bot"); bots.Total != 1 || entryNames(bots.Entries) != "[1 Kraken, by Eli]" {
		t.Fatalf("bots = %+v", bots)
	}
	if manual := readLeaderboard(t, api, "?kind=manual&limit=1"); manual.Total != 1 || entryNames(manual.Entries) != "[1 Dana]" {
		t.Fatalf("hand play = %+v", manual)
	}
	for _, query := range []string{"?kind=guest", "?limit=0", "?limit=101", "?offset=-1"} {
		expectStatus(t, api.Get("/leaderboard"+query), http.StatusUnprocessableEntity, nil)
	}
}

// A bot is listed with its owner's display name, which follows a rename in
// the lobby once the owner calls the game again.
func TestABotIsListedWithItsOwnersDisplayName(t *testing.T) {
	database := testutils.InitTestDB(t)
	dana := signedInPlayer("Dana")
	renamed := dana
	renamed.DisplayName = "Dana B"
	api := leaderboardAPI(t, database, bearerPlayers{"Bearer old": dana, "Bearer new": renamed}, testAdminAPIKey)
	kraken := registerBotName(t, api, "old", "Kraken")
	hand := handOf(t, api, "old")
	played(t, database, kraken.ID, 1520, 1)
	played(t, database, hand.ID, 1480, 1)
	if got := entryNames(readLeaderboard(t, api, "").Entries); got != "[1 Kraken, by Dana 2 Dana]" {
		t.Fatalf("leaderboard = %s", got)
	}

	handOf(t, api, "new")
	if got := entryNames(readLeaderboard(t, api, "").Entries); got != "[1 Kraken, by Dana B 2 Dana B]" {
		t.Fatalf("leaderboard after the rename = %s", got)
	}
	var page models.LeaderboardCompetitorView
	expectStatus(t, api.Get("/leaderboard/"+kraken.ID), http.StatusOK, &page)
	if page.Owner == nil || *page.Owner != "Dana B" {
		t.Fatalf("Kraken's page = %+v", page.LeaderboardEntryView)
	}
}

// historyFixture is Kraken, Dana's bot, with three rated games: a win
// against the house bot of level 2, a loss against Eli's hand play, and a
// win against Nessie, Fay's bot, newest last.
type historyFixture struct {
	api                         humatest.TestAPI
	database                    *gorm.DB
	eli                         string
	kraken, eliHand, nessie     models.CompetitorView
	house                       models.Competitor
	firstEnd, secondEnd, latest time.Time
}

func newHistoryFixture(t *testing.T) historyFixture {
	t.Helper()
	database := testutils.InitTestDB(t)
	eli := signedInPlayer("Eli")
	fixture := historyFixture{database: database, eli: eli.AccountID}
	fixture.api = leaderboardAPI(t, database, bearerPlayers{"Bearer dana": signedInPlayer("Dana"), "Bearer eli": eli, "Bearer fay": signedInPlayer("Fay")}, testAdminAPIKey)
	fixture.kraken = registerBotName(t, fixture.api, "dana", "Kraken")
	fixture.eliHand = handOf(t, fixture.api, "eli")
	fixture.nessie = registerBotName(t, fixture.api, "fay", "Nessie")
	fixture.house = houseBot(t, database, 2)
	played(t, database, fixture.kraken.ID, 1530.4, 3)
	played(t, database, fixture.eliHand.ID, 1519.6, 1)
	played(t, database, fixture.nessie.ID, 1480, 1)
	played(t, database, fixture.house.ID, 1480, 1)

	fixture.firstEnd = time.Now().Add(-3 * time.Hour).UTC().Truncate(time.Microsecond)
	fixture.secondEnd, fixture.latest = fixture.firstEnd.Add(time.Hour), fixture.firstEnd.Add(2*time.Hour)
	games := []struct {
		opponent      string
		result        models.RatingResult
		before, after float64
		endedAt       time.Time
	}{
		{fixture.house.ID, models.RatingWin, 1500, 1520, fixture.firstEnd},
		{fixture.eliHand.ID, models.RatingLoss, 1520, 1500.4, fixture.secondEnd},
		{fixture.nessie.ID, models.RatingWin, 1500.4, 1530.4, fixture.latest},
	}
	events := []models.RatingEvent{}
	for index, game := range games {
		gameID := uint(index + 1)
		opponentResult := models.RatingLoss
		if game.result == models.RatingLoss {
			opponentResult = models.RatingWin
		}
		events = append(events,
			models.RatingEvent{GameID: gameID, CompetitorID: fixture.kraken.ID, Result: game.result, RatingBefore: game.before, RatingAfter: game.after, K: 40,
				Opponents: []models.RatingOpponent{{CompetitorID: game.opponent, RatingBefore: 1500}}, EndedAt: game.endedAt},
			models.RatingEvent{GameID: gameID, CompetitorID: game.opponent, Result: opponentResult, RatingBefore: 1500, RatingAfter: 1500 + game.before - game.after, K: 40,
				Opponents: []models.RatingOpponent{{CompetitorID: fixture.kraken.ID, RatingBefore: game.before}}, EndedAt: game.endedAt})
	}
	if err := dal.StoreRatingEvents(database, events); err != nil {
		t.Fatal(err)
	}
	return fixture
}

func (fixture historyFixture) page(t *testing.T, competitorID string) models.LeaderboardCompetitorView {
	t.Helper()
	var page models.LeaderboardCompetitorView
	expectStatus(t, fixture.api.Get("/leaderboard/"+competitorID), http.StatusOK, &page)
	return page
}

// historyNames shows a page's history as each game's result and opponents.
func historyNames(history []models.CompetitorResultView) string {
	shown := []string{}
	for _, result := range history {
		for _, opponent := range result.Opponents {
			name := opponent.Name
			if opponent.Owner != nil {
				name += ", by " + *opponent.Owner
			}
			if opponent.CompetitorID == nil {
				name += " (no ID)"
			}
			shown = append(shown, fmt.Sprintf("%s against %s", result.Result, name))
		}
	}
	return fmt.Sprint(shown)
}

func TestACompetitorsPageListsItsResultsNewestFirst(t *testing.T) {
	fixture := newHistoryFixture(t)
	page := fixture.page(t, fixture.kraken.ID)
	if page.CompetitorID != fixture.kraken.ID || page.Rank != 1 || page.Name != "Kraken" || page.Owner == nil || *page.Owner != "Dana" || page.Rating != 1530 || page.Games != 3 {
		t.Fatalf("Kraken's entry = %+v", page.LeaderboardEntryView)
	}
	if got := historyNames(page.History); got != "[win against Nessie, by Fay loss against Eli win against Bot (level 2)]" {
		t.Fatalf("Kraken's history = %s", got)
	}
	latest, second := page.History[0], page.History[1]
	if !latest.EndedAt.Equal(fixture.latest) || latest.RatingBefore != 1500 || latest.RatingAfter != 1530 || latest.Opponents[0].RatingBefore != 1500 ||
		*latest.Opponents[0].CompetitorID != fixture.nessie.ID || second.RatingBefore != 1520 || second.RatingAfter != 1500 {
		t.Fatalf("Kraken's latest results = %+v", page.History[:2])
	}
	// Ranked among every kind: Eli's 1519.6 shows as 1520, behind Kraken,
	// and the house bot ties with Nessie.
	if eli := fixture.page(t, fixture.eliHand.ID); eli.Rank != 2 || eli.Rating != 1520 || eli.Owner != nil || historyNames(eli.History) != "[win against Kraken, by Dana]" {
		t.Fatalf("Eli's page = %+v", eli)
	}
	if house := fixture.page(t, fixture.house.ID); house.Rank != 3 || house.Name != "Bot (level 2)" || house.Kind != models.HouseCompetitor || house.Owner != nil {
		t.Fatalf("the house bot's page = %+v", house.LeaderboardEntryView)
	}

	// Fay deletes Nessie: Kraken's history keeps the game, without her name.
	expectStatus(t, fixture.api.Delete("/account/bot-names/"+fixture.nessie.ID, "Authorization: Bearer fay"), http.StatusNoContent, nil)
	if got := historyNames(fixture.page(t, fixture.kraken.ID).History); got != "[win against Deleted bot (no ID) loss against Eli win against Bot (level 2)]" {
		t.Fatalf("Kraken's history after Nessie's deletion = %s", got)
	}
	expectStatus(t, fixture.api.Get("/leaderboard/"+fixture.nessie.ID), http.StatusNotFound, nil)
	expectStatus(t, fixture.api.Get("/leaderboard/"+uuid.NewString()), http.StatusNotFound, nil)
	expectStatus(t, fixture.api.Get("/leaderboard/not-an-id"), http.StatusUnprocessableEntity, nil)
}

// The admin removes a bot name: it leaves the leaderboard, and the name is
// free for its owner to register another. A person's hand play and a house
// bot can't be removed.
func TestTheAdminRemovesABotName(t *testing.T) {
	fixture := newHistoryFixture(t)
	remove := func(competitorID string) *httptest.ResponseRecorder {
		return fixture.api.Delete("/admin/competitors/"+competitorID, adminHeader)
	}
	expectStatus(t, remove(fixture.kraken.ID), http.StatusNoContent, nil)
	if got := entryNames(readLeaderboard(t, fixture.api, "").Entries); got != "[1 Eli 2 Nessie, by Fay 2 Bot (level 2)]" {
		t.Fatalf("leaderboard = %s", got)
	}
	expectStatus(t, fixture.api.Get("/leaderboard/"+fixture.kraken.ID), http.StatusNotFound, nil)
	if got := historyNames(fixture.page(t, fixture.eliHand.ID).History); got != "[win against Deleted bot (no ID)]" {
		t.Fatalf("Eli's history = %s", got)
	}
	again := registerBotName(t, fixture.api, "dana", "kraken")
	if again.ID == fixture.kraken.ID || again.Rating != 1500 {
		t.Fatalf("Kraken again = %+v", again)
	}

	expectStatus(t, remove(fixture.kraken.ID), http.StatusNotFound, nil)
	expectStatus(t, remove(uuid.NewString()), http.StatusNotFound, nil)
	expectStatus(t, remove(fixture.eliHand.ID), http.StatusUnprocessableEntity, nil)
	expectStatus(t, remove(fixture.house.ID), http.StatusUnprocessableEntity, nil)
	expectStatus(t, remove("not-an-id"), http.StatusUnprocessableEntity, nil)
	if got := entryNames(readLeaderboard(t, fixture.api, "").Entries); got != "[1 Eli 2 Nessie, by Fay 2 Bot (level 2)]" {
		t.Fatalf("leaderboard after the refusals = %s", got)
	}
}

// The admin hides an account: its hand play and its bots leave the
// leaderboard and other competitors' history, and come back when it is
// shown again. Nothing is deleted, and the account still plays.
func TestTheAdminHidesAnAccount(t *testing.T) {
	fixture := newHistoryFixture(t)
	eliBot := registerBotName(t, fixture.api, "eli", "Hydra")
	played(t, fixture.database, eliBot.ID, 1490, 1)
	hide := func(accountID string, hidden bool) *httptest.ResponseRecorder {
		return fixture.api.Put("/admin/accounts/"+accountID+"/hidden", adminHeader, map[string]bool{"hidden": hidden})
	}
	const everyone = "[1 Kraken, by Dana 2 Eli 3 Hydra, by Eli 4 Nessie, by Fay 4 Bot (level 2)]"
	if got := entryNames(readLeaderboard(t, fixture.api, "").Entries); got != everyone {
		t.Fatalf("leaderboard = %s", got)
	}

	expectStatus(t, hide(fixture.eli, true), http.StatusNoContent, nil)
	if leaderboard := readLeaderboard(t, fixture.api, ""); leaderboard.Total != 3 || entryNames(leaderboard.Entries) != "[1 Kraken, by Dana 2 Nessie, by Fay 2 Bot (level 2)]" {
		t.Fatalf("leaderboard with Eli hidden = %d: %s", leaderboard.Total, entryNames(leaderboard.Entries))
	}
	expectStatus(t, fixture.api.Get("/leaderboard/"+fixture.eliHand.ID), http.StatusNotFound, nil)
	expectStatus(t, fixture.api.Get("/leaderboard/"+eliBot.ID), http.StatusNotFound, nil)
	if got := historyNames(fixture.page(t, fixture.kraken.ID).History); got != "[win against Nessie, by Fay loss against Deleted player (no ID) win against Bot (level 2)]" {
		t.Fatalf("Kraken's history with Eli hidden = %s", got)
	}
	// Eli still plays, and a call of the game doesn't show the account.
	var account models.IslandAccountView
	expectStatus(t, fixture.api.Get("/account", "Authorization: Bearer eli"), http.StatusOK, &account)
	if len(account.BotNames) != 1 || account.Hand.Rating != 1519.6 {
		t.Fatalf("Eli's account = %+v", account)
	}
	if leaderboard := readLeaderboard(t, fixture.api, ""); leaderboard.Total != 3 {
		t.Fatalf("leaderboard after Eli's call = %s", entryNames(leaderboard.Entries))
	}

	expectStatus(t, hide(fixture.eli, false), http.StatusNoContent, nil)
	if got := entryNames(readLeaderboard(t, fixture.api, "").Entries); got != everyone {
		t.Fatalf("leaderboard with Eli shown again = %s", got)
	}
	if got := historyNames(fixture.page(t, fixture.kraken.ID).History); got != "[win against Nessie, by Fay loss against Eli win against Bot (level 2)]" {
		t.Fatalf("Kraken's history with Eli shown again = %s", got)
	}

	expectStatus(t, hide(uuid.NewString(), true), http.StatusNotFound, nil)
	expectStatus(t, fixture.api.Put("/admin/accounts/"+fixture.eli+"/hidden", adminHeader, map[string]any{}), http.StatusUnprocessableEntity, nil)
}

func TestTheAdminRoutesNeedTheAdminAPIKey(t *testing.T) {
	database := testutils.InitTestDB(t)
	dana := signedInPlayer("Dana")
	players := bearerPlayers{"Bearer dana": dana}
	api := leaderboardAPI(t, database, players, testAdminAPIKey)
	kraken := registerBotName(t, api, "dana", "Kraken")
	played(t, database, kraken.ID, 1520, 1)
	hidden := map[string]bool{"hidden": true}
	for _, headers := range [][]any{{}, {"Authorization: Bearer dana"}, {routes.AdminAPIKeyHeader + ": wrong"}} {
		expectStatus(t, api.Delete("/admin/competitors/"+kraken.ID, headers...), http.StatusUnauthorized, nil)
		expectStatus(t, api.Put("/admin/accounts/"+dana.AccountID+"/hidden", append(headers, hidden)...), http.StatusUnauthorized, nil)
	}
	// Without a key, the deployment refuses every admin request.
	disabled := leaderboardAPI(t, database, players, "")
	expectStatus(t, disabled.Delete("/admin/competitors/"+kraken.ID, adminHeader), http.StatusForbidden, nil)
	expectStatus(t, disabled.Put("/admin/accounts/"+dana.AccountID+"/hidden", adminHeader, hidden), http.StatusForbidden, nil)
	if leaderboard := readLeaderboard(t, api, ""); leaderboard.Total != 1 {
		t.Fatalf("leaderboard = %s, want Kraken still listed", entryNames(leaderboard.Entries))
	}
}

func TestTheLeaderboardIsPublicAndCachedForAMinute(t *testing.T) {
	database := testutils.InitTestDB(t)
	api := leaderboardAPI(t, database, bearerPlayers{"Bearer dana": signedInPlayer("Dana")}, testAdminAPIKey)
	kraken := registerBotName(t, api, "dana", "Kraken")
	played(t, database, kraken.ID, 1520, 1)
	for _, path := range []string{"/leaderboard", "/leaderboard?kind=bot", "/leaderboard/" + kraken.ID} {
		response := api.Get(path)
		expectStatus(t, response, http.StatusOK, nil)
		if cacheControl := response.Header().Get("Cache-Control"); cacheControl != "public, max-age=60" {
			t.Fatalf("%s: Cache-Control = %q", path, cacheControl)
		}
	}
}
