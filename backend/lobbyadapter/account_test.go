package lobbyadapter_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/INTODAN/mpl/domain"
	"islandBattles/backend/lobbyadapter"
	"islandBattles/backend/models"
	"islandBattles/backend/services"
)

// signIn signs a new Google account in to the lobby with a display name,
// and returns its session.
func (stack *lobbyStack) signIn(subject, displayName string) (guest, domain.Account) {
	stack.t.Helper()
	credential, err := stack.google.GoogleToken(subject, googleClientID, time.Now())
	if err != nil {
		stack.t.Fatal(err)
	}
	var session domain.AccountSession
	stack.expect(stack.api.Post("/lobby/v1/sessions/google", map[string]string{"credential": credential, "display_name": displayName}), http.StatusCreated, &session)
	return guest{playerID: session.PlayerID, header: "Authorization: Bearer " + session.AccessToken}, session.Account
}

// A signed-in player's token names their account, which the game stores
// with its hand play; a guest's names none. A rename in the lobby reaches
// the game with the next token.
func TestTheGameLearnsTheAccountFromThePlayerToken(t *testing.T) {
	stack := newLobbyStack(t)
	dana, account := stack.signIn("google-dana", "Dana")
	token := stack.playerToken(dana, lobbyadapter.GameKey)

	var view models.IslandAccountView
	stack.expect(stack.api.Get("/account", token), http.StatusOK, &view)
	if view.AccountID != account.ID || view.DisplayName != "Dana" || view.Hand.Kind != models.ManualCompetitor || view.Hand.Name != "Dana" ||
		view.Hand.Rating != 1500 || len(view.BotNames) != 0 {
		t.Fatalf("account = %+v, want Dana's with her hand play at 1500 and no bot names", view)
	}
	stack.expect(stack.api.Post("/account/bot-names", token, map[string]string{"name": "Kraken"}), http.StatusCreated, nil)

	var problem struct {
		Type string `json:"type"`
	}
	stack.expect(stack.api.Get("/account", stack.playerToken(stack.guest("Guest"), lobbyadapter.GameKey)), http.StatusForbidden, &problem)
	if problem.Type != services.SignInRequiredProblemType {
		t.Fatalf("guest's problem = %+v", problem)
	}

	var renamed domain.Account
	stack.expect(stack.api.Put("/lobby/v1/account", dana.header, map[string]string{"display_name": "Dana B"}), http.StatusOK, &renamed)
	// The old token still names the old name.
	var stored models.IslandAccount
	stack.expect(stack.api.Get("/account", token), http.StatusOK, nil)
	if err := stack.database.Where(&models.IslandAccount{AccountID: account.ID}).First(&stored).Error; err != nil {
		t.Fatal(err)
	}
	if stored.DisplayName != "Dana" {
		t.Fatalf("display name before a new token = %q", stored.DisplayName)
	}
	stack.expect(stack.api.Get("/account", stack.playerToken(dana, lobbyadapter.GameKey)), http.StatusOK, &view)
	if err := stack.database.Where(&models.IslandAccount{AccountID: account.ID}).First(&stored).Error; err != nil {
		t.Fatal(err)
	}
	if stored.DisplayName != "Dana B" || view.DisplayName != "Dana B" || view.Hand.Name != "Dana B" || len(view.BotNames) != 1 {
		t.Fatalf("after the rename, stored = %+v and account = %+v", stored, view)
	}
}
