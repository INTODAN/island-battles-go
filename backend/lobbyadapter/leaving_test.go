package lobbyadapter_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/INTODAN/mpl/domain"
	"islandBattles/backend/lobbyadapter"
	"islandBattles/backend/models"

	"gorm.io/gorm"
)

// startByHand starts a two-team lobby whose members both play by hand.
func startByHand(t *testing.T, stack *lobbyStack) (domain.Snapshot, guest, guest) {
	t.Helper()
	alice, bob := stack.guest("Alice"), stack.guest("Bob")
	room := stack.room(lobbyadapter.GameKey, alice, bob)
	for _, member := range []guest{alice, bob} {
		stack.expect(stack.choose(room, member, `{"kind":"manual"}`), http.StatusOK, nil)
		stack.expect(stack.ready(room, member), http.StatusOK, nil)
	}
	stack.expect(stack.api.Post(stack.lobbyPath(room)+"/start", alice.header, map[string]any{}), http.StatusOK, nil)
	return room, alice, bob
}

// ageOpenGeneration moves the game's open generation back by age, as if
// nobody had called the game since.
func ageOpenGeneration(t *testing.T, database *gorm.DB, generationID uint, age time.Duration) {
	t.Helper()
	if err := database.Model(&models.Generation{}).Where(&models.Generation{Model: gorm.Model{ID: generationID}}).Update("created_at", time.Now().Add(-age)).Error; err != nil {
		t.Fatal(err)
	}
}

// With every tab closed for longer than the turn limit, the lobby's reopen
// succeeds: the round-ended hook ends the abandoned game, without a turn.
func TestReopenEndsAnAbandonedRealTimeGame(t *testing.T) {
	stack := newLobbyStack(t)
	room, alice, bob := startByHand(t, stack)
	state := stack.state(room, alice)
	var refusal problem
	stack.expect(stack.api.Post(stack.lobbyPath(room)+"/reopen", bob.header), http.StatusConflict, &refusal)
	if refusal.Code != "game_running" {
		t.Fatalf("reopen during the game = %+v", refusal)
	}
	ageOpenGeneration(t, stack.database, state.GenerationID, time.Hour)
	var reopened domain.Snapshot
	stack.expect(stack.api.Post(stack.lobbyPath(room)+"/reopen", bob.header), http.StatusOK, &reopened)
	if reopened.Lobby.Phase != domain.PhaseWaiting || reopened.Lobby.Round != 2 {
		t.Fatalf("reopened = %+v", reopened.Lobby)
	}
	ended := stack.state(room, alice)
	if ended.Outcome == nil || ended.Outcome.Reason != models.GameOutcomeAbandoned || ended.GenerationID != state.GenerationID {
		t.Fatalf("the game after the reopen = %+v, outcome %+v", ended, ended.Outcome)
	}
}

// After a resignation has played, the round is over and the lobby reopens.
func TestReopenAfterAResignation(t *testing.T) {
	stack := newLobbyStack(t)
	room, alice, bob := startByHand(t, stack)
	state := stack.state(room, alice)
	stack.expect(stack.api.Post("/game/from-lobby/"+room.Lobby.ID+"/resign", stack.playerToken(alice, lobbyadapter.GameKey), map[string]any{}), http.StatusOK, nil)
	// Bob's client calls the deadline once the turn's time is up.
	ageOpenGeneration(t, stack.database, state.GenerationID, 2*time.Second)
	var ended models.GenerationState
	stack.expect(stack.api.Post("/game/from-lobby/"+room.Lobby.ID+"/deadline", stack.playerToken(bob, lobbyadapter.GameKey), map[string]any{}), http.StatusOK, &ended)
	if ended.Outcome == nil || ended.Outcome.Reason != models.GameOutcomeLastTeamStanding || ended.Outcome.Summary != "Red resigned; Blue wins as the last team standing" {
		t.Fatalf("outcome = %+v", ended.Outcome)
	}
	stack.expect(stack.api.Post(stack.lobbyPath(room)+"/reopen", bob.header), http.StatusOK, nil)
}
