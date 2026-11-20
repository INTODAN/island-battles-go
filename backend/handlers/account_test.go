package handlers_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/INTODAN/mpl/domain"
	"github.com/danielgtaylor/huma/v2/humatest"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"islandBattles/backend/handlers"
	"islandBattles/backend/internal/testutils"
	"islandBattles/backend/models"
	"islandBattles/backend/routes"
	"islandBattles/backend/services"
)

// accountProblem is a problem answer of the account routes.
type accountProblem struct {
	Type   string `json:"type"`
	Status int    `json:"status"`
	Detail string `json:"detail"`
}

// accountAPI serves the account routes to the players that players names by
// their whole Authorization header.
func accountAPI(t *testing.T, database *gorm.DB, players bearerPlayers) humatest.TestAPI {
	t.Helper()
	_, api := humatest.New(t, routes.NewAPIConfig("test", "1.0.0"))
	routes.SetupAccountRoutes(api, &handlers.Accounts{Players: players, Service: services.Accounts{Database: database}})
	return api
}

// signedInPlayer is a player token of a new player of a new account.
func signedInPlayer(name string) domain.Principal {
	return domain.Principal{PlayerID: uuid.NewString(), DisplayName: name, AccountID: uuid.NewString()}
}

func expectStatus(t *testing.T, response *httptest.ResponseRecorder, status int, into any) {
	t.Helper()
	if response.Code != status {
		t.Fatalf("status %d, want %d: %s", response.Code, status, response.Body.String())
	}
	if into != nil {
		if err := json.Unmarshal(response.Body.Bytes(), into); err != nil {
			t.Fatal(err)
		}
	}
}

func expectProblem(t *testing.T, response *httptest.ResponseRecorder, status int, problemType string) {
	t.Helper()
	var problem accountProblem
	expectStatus(t, response, status, &problem)
	if problem.Type != problemType {
		t.Fatalf("problem = %+v, want type %s", problem, problemType)
	}
}

func TestASignedInPlayerHasAnAccountAndAGuestHasNone(t *testing.T) {
	database := testutils.InitTestDB(t)
	dana := signedInPlayer("Dana")
	api := accountAPI(t, database, bearerPlayers{
		"Bearer dana":  dana,
		"Bearer guest": {PlayerID: uuid.NewString(), DisplayName: "Guest"},
	})

	var account models.IslandAccountView
	expectStatus(t, api.Get("/account", "Authorization: Bearer dana"), http.StatusOK, &account)
	if account.AccountID != dana.AccountID || account.DisplayName != "Dana" || account.MaxBotNames != models.MaxBotNames || len(account.BotNames) != 0 {
		t.Fatalf("account = %+v", account)
	}
	hand := account.Hand
	if hand.Kind != models.ManualCompetitor || hand.Name != "Dana" || hand.Rating != 1500 || hand.Games != 0 || hand.LastPlayedAt != nil || hand.ID == "" {
		t.Fatalf("hand = %+v, want Dana's manual competitor at 1500", hand)
	}
	// Reading again keeps the same hand play.
	var again models.IslandAccountView
	expectStatus(t, api.Get("/account", "Authorization: Bearer dana"), http.StatusOK, &again)
	if again.Hand.ID != hand.ID {
		t.Fatalf("hand = %s, then %s", hand.ID, again.Hand.ID)
	}

	for name, response := range map[string]*httptest.ResponseRecorder{
		"read":           api.Get("/account", "Authorization: Bearer guest"),
		"register":       api.Post("/account/bot-names", "Authorization: Bearer guest", map[string]string{"name": "Kraken"}),
		"delete a name":  api.Delete("/account/bot-names/"+uuid.NewString(), "Authorization: Bearer guest"),
		"delete account": api.Delete("/account", "Authorization: Bearer guest"),
	} {
		t.Run(name, func(t *testing.T) {
			expectProblem(t, response, http.StatusForbidden, services.SignInRequiredProblemType)
		})
	}
	expectStatus(t, api.Get("/account"), http.StatusUnauthorized, nil)
	var accounts, competitors int64
	database.Model(&models.IslandAccount{}).Count(&accounts)
	database.Model(&models.Competitor{}).Count(&competitors)
	if accounts != 1 || competitors != 1 {
		t.Fatalf("%d accounts and %d competitors, want only Dana's", accounts, competitors)
	}
}

func TestBotNamesAreUniqueAndAtMostFive(t *testing.T) {
	database := testutils.InitTestDB(t)
	dana, eli := signedInPlayer("Dana"), signedInPlayer("Eli")
	api := accountAPI(t, database, bearerPlayers{"Bearer dana": dana, "Bearer eli": eli})
	register := func(token, name string) *httptest.ResponseRecorder {
		return api.Post("/account/bot-names", "Authorization: Bearer "+token, map[string]string{"name": name})
	}

	var kraken models.CompetitorView
	expectStatus(t, register("dana", "  Kraken "), http.StatusCreated, &kraken)
	if kraken.Kind != models.BotCompetitor || kraken.Name != "Kraken" || kraken.Rating != 1500 || kraken.Games != 0 {
		t.Fatalf("Kraken = %+v", kraken)
	}
	expectProblem(t, register("dana", "Kraken"), http.StatusConflict, services.BotNameTakenProblemType)
	expectProblem(t, register("eli", "kraken"), http.StatusConflict, services.BotNameTakenProblemType)
	for _, name := range []string{"ab", "Kraken!", "Bot (level 2)", "   ", "A name much longer than 24"} {
		var problem accountProblem
		expectStatus(t, register("eli", name), http.StatusUnprocessableEntity, &problem)
	}

	for index := range models.MaxBotNames - 1 {
		expectStatus(t, register("dana", fmt.Sprintf("Kraken %d", index+2)), http.StatusCreated, nil)
	}
	expectProblem(t, register("dana", "Leviathan"), http.StatusConflict, services.BotNameLimitProblemType)
	// The limit is the account's own.
	expectStatus(t, register("eli", "Leviathan"), http.StatusCreated, nil)

	var account models.IslandAccountView
	expectStatus(t, api.Get("/account", "Authorization: Bearer dana"), http.StatusOK, &account)
	names := []string{}
	for _, botName := range account.BotNames {
		names = append(names, botName.Name)
	}
	if fmt.Sprint(names) != "[Kraken Kraken 2 Kraken 3 Kraken 4 Kraken 5]" {
		t.Fatalf("bot names = %v, want Dana's five, oldest first", names)
	}
}

// Names registered at once count each other, so none goes past the limit.
func TestBotNamesRegisteredAtOnceKeepTheLimit(t *testing.T) {
	database := testutils.InitTestDB(t)
	dana := signedInPlayer("Dana")
	api := accountAPI(t, database, bearerPlayers{"Bearer dana": dana})
	const attempts = 8
	codes := make(chan int, attempts)
	var group sync.WaitGroup
	for index := range attempts {
		group.Go(func() {
			codes <- api.Post("/account/bot-names", "Authorization: Bearer dana", map[string]string{"name": fmt.Sprintf("Racer %d", index)}).Code
		})
	}
	group.Wait()
	close(codes)
	created := 0
	for code := range codes {
		switch code {
		case http.StatusCreated:
			created++
		case http.StatusConflict:
		default:
			t.Errorf("status %d", code)
		}
	}
	if created != models.MaxBotNames {
		t.Fatalf("%d names registered, want %d", created, models.MaxBotNames)
	}
}

func TestDeletingABotNameFreesIt(t *testing.T) {
	database := testutils.InitTestDB(t)
	dana, eli := signedInPlayer("Dana"), signedInPlayer("Eli")
	api := accountAPI(t, database, bearerPlayers{"Bearer dana": dana, "Bearer eli": eli})
	var kraken models.CompetitorView
	expectStatus(t, api.Post("/account/bot-names", "Authorization: Bearer dana", map[string]string{"name": "Kraken"}), http.StatusCreated, &kraken)
	// A rated game moved it from 1500.
	if err := database.Model(&models.Competitor{}).Where(&models.Competitor{ID: kraken.ID}).Updates(map[string]any{"rating": 1520, "games": 1, "wins": 1}).Error; err != nil {
		t.Fatal(err)
	}

	expectStatus(t, api.Delete("/account/bot-names/"+kraken.ID, "Authorization: Bearer eli"), http.StatusNotFound, nil)
	expectStatus(t, api.Delete("/account/bot-names/"+uuid.NewString(), "Authorization: Bearer dana"), http.StatusNotFound, nil)
	var account models.IslandAccountView
	expectStatus(t, api.Get("/account", "Authorization: Bearer dana"), http.StatusOK, &account)
	expectStatus(t, api.Delete("/account/bot-names/"+account.Hand.ID, "Authorization: Bearer dana"), http.StatusNotFound, nil)

	expectStatus(t, api.Delete("/account/bot-names/"+kraken.ID, "Authorization: Bearer dana"), http.StatusNoContent, nil)
	expectStatus(t, api.Delete("/account/bot-names/"+kraken.ID, "Authorization: Bearer dana"), http.StatusNotFound, nil)
	var again models.CompetitorView
	expectStatus(t, api.Post("/account/bot-names", "Authorization: Bearer eli", map[string]string{"name": "KRAKEN"}), http.StatusCreated, &again)
	if again.ID == kraken.ID || again.Rating != 1500 || again.Games != 0 || again.Name != "KRAKEN" {
		t.Fatalf("Kraken again = %+v, want a new competitor at 1500", again)
	}
	expectStatus(t, api.Get("/account", "Authorization: Bearer dana"), http.StatusOK, &account)
	if len(account.BotNames) != 0 {
		t.Fatalf("Dana's bot names = %+v, want none", account.BotNames)
	}
	// The deleted row stays, without its name, for a game in play.
	var deleted models.Competitor
	if err := database.Unscoped().Where(&models.Competitor{ID: kraken.ID}).First(&deleted).Error; err != nil {
		t.Fatal(err)
	}
	if !deleted.DeletedAt.Valid || deleted.Name != nil || deleted.NameKey != nil || deleted.Rating != 1520 {
		t.Fatalf("deleted Kraken = %+v", deleted)
	}
}

func TestDisplayNameFollowsTheToken(t *testing.T) {
	database := testutils.InitTestDB(t)
	dana := signedInPlayer("Dana")
	renamed := dana
	renamed.DisplayName = "Dana B"
	api := accountAPI(t, database, bearerPlayers{"Bearer old": dana, "Bearer new": renamed})
	expectStatus(t, api.Get("/account", "Authorization: Bearer old"), http.StatusOK, nil)
	var account models.IslandAccountView
	expectStatus(t, api.Get("/account", "Authorization: Bearer new"), http.StatusOK, &account)
	if account.DisplayName != "Dana B" || account.Hand.Name != "Dana B" {
		t.Fatalf("account = %+v, want Dana B", account)
	}
	var stored models.IslandAccount
	if err := database.Where(&models.IslandAccount{AccountID: dana.AccountID}).First(&stored).Error; err != nil {
		t.Fatal(err)
	}
	if stored.DisplayName != "Dana B" || !stored.UpdatedAt.After(stored.CreatedAt) {
		t.Fatalf("stored account = %+v, want renamed", stored)
	}
}

func TestDeletingAnAccountLeavesNoLiveCompetitorAndNoAccountRow(t *testing.T) {
	database := testutils.InitTestDB(t)
	dana, eli := signedInPlayer("Dana"), signedInPlayer("Eli")
	api := accountAPI(t, database, bearerPlayers{"Bearer dana": dana, "Bearer eli": eli})
	for _, name := range []string{"Kraken", "Leviathan"} {
		expectStatus(t, api.Post("/account/bot-names", "Authorization: Bearer dana", map[string]string{"name": name}), http.StatusCreated, nil)
	}
	expectStatus(t, api.Post("/account/bot-names", "Authorization: Bearer eli", map[string]string{"name": "Nessie"}), http.StatusCreated, nil)

	expectStatus(t, api.Delete("/account", "Authorization: Bearer dana"), http.StatusNoContent, nil)
	var accounts, live, kept int64
	database.Model(&models.IslandAccount{}).Where(&models.IslandAccount{AccountID: dana.AccountID}).Count(&accounts)
	database.Model(&models.Competitor{}).Where(&models.Competitor{AccountID: &dana.AccountID}).Count(&live)
	database.Unscoped().Model(&models.Competitor{}).Where(&models.Competitor{AccountID: &dana.AccountID}).Count(&kept)
	if accounts != 0 || live != 0 || kept != 3 {
		t.Fatalf("%d account rows, %d live and %d kept competitors, want 0, 0 and 3", accounts, live, kept)
	}
	// Eli's account is untouched, and Dana's names are free.
	var account models.IslandAccountView
	expectStatus(t, api.Get("/account", "Authorization: Bearer eli"), http.StatusOK, &account)
	if len(account.BotNames) != 1 {
		t.Fatalf("Eli's account = %+v", account)
	}
	expectStatus(t, api.Post("/account/bot-names", "Authorization: Bearer eli", map[string]string{"name": "kraken"}), http.StatusCreated, nil)
	// Deleting it again is 204 too.
	expectStatus(t, api.Delete("/account", "Authorization: Bearer dana"), http.StatusNoContent, nil)
}
