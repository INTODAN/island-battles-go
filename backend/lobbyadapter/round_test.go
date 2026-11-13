package lobbyadapter_test

import (
	"net/http"
	"testing"

	"github.com/INTODAN/mpl/domain"
	"github.com/INTODAN/mpl/gamehooks"
	"islandBattles/backend/dal"
	"islandBattles/backend/lobbyadapter"
	"islandBattles/backend/models"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// endGame gives a game an outcome, as its last turn does.
func endGame(t *testing.T, database *gorm.DB, gameID uint) {
	t.Helper()
	outcome := &models.GameOutcome{Reason: models.GameOutcomeAllEliminated}
	if err := database.Model(&models.Game{Model: gorm.Model{ID: gameID}}).Updates(&models.Game{Outcome: outcome}).Error; err != nil {
		t.Fatal(err)
	}
}

// A lobby plays its first game, a member reopens it once the game has ended,
// and the host starts a second game in it, which is a new game that only its
// own roster plays.
func TestALobbyPlaysASecondGameAfterTheFirstEnds(t *testing.T) {
	stack := newLobbyStack(t)
	alice, bob, carol := stack.guest("Alice"), stack.guest("Bob"), stack.guest("Carol")
	var room domain.Snapshot
	stack.expect(stack.api.Post("/lobby/v1/lobbies", alice.header, map[string]any{
		"request_id": uuid.NewString(), "game_key": lobbyadapter.GameKey, "name": "Rematch", "teams": teamBodies("Red", "Blue", "Green"),
		"client_instance_id": alice.client,
	}), http.StatusCreated, &room)
	if room.Lobby.Round != 1 {
		t.Fatalf("a new lobby's round = %d, want 1", room.Lobby.Round)
	}
	for index, member := range []guest{alice, bob, carol} {
		if member != alice {
			stack.expect(stack.api.Post("/lobby/v1/memberships", member.header, map[string]any{"game_key": lobbyadapter.GameKey, "join_code": room.Lobby.JoinCode, "client_instance_id": member.client}), http.StatusOK, nil)
		}
		stack.expect(stack.api.Put(stack.lobbyPath(room)+"/team", member.header, map[string]any{"team_id": room.Teams[index].ID}), http.StatusOK, nil)
		stack.expect(stack.choose(room, member, `{"kind":"localhost"}`), http.StatusOK, nil)
		stack.expect(stack.ready(room, member), http.StatusOK, nil)
	}
	reopen := func(member guest, status int, into any) {
		t.Helper()
		stack.expect(stack.api.Post(stack.lobbyPath(room)+"/reopen", member.header), status, into)
	}
	start := func() domain.Snapshot {
		t.Helper()
		var started domain.Snapshot
		stack.expect(stack.api.Post(stack.lobbyPath(room)+"/start", alice.header, map[string]any{}), http.StatusOK, &started)
		return started
	}

	first := start()
	firstGame := stack.state(room, carol)
	if first.Lobby.Round != 1 || firstGame.LobbyRound != 1 {
		t.Fatalf("first start = round %d, game round %d", first.Lobby.Round, firstGame.LobbyRound)
	}
	var refusal problem
	reopen(bob, http.StatusConflict, &refusal)
	if refusal.Code != "game_running" {
		t.Fatalf("reopen during the game = %+v", refusal)
	}

	endGame(t, stack.database, firstGame.GameID)
	var reopened, again domain.Snapshot
	reopen(bob, http.StatusOK, &reopened)
	if reopened.Lobby.Phase != domain.PhaseWaiting || reopened.Lobby.Round != 2 || len(reopened.StartRoster) != 0 || len(reopened.Teams) != 3 {
		t.Fatalf("reopened = %+v", reopened.Lobby)
	}
	reopen(alice, http.StatusOK, &again)
	if again.Lobby.Version != reopened.Lobby.Version || again.Lobby.Round != 2 {
		t.Fatalf("second reopen = version %d round %d, want %d round 2", again.Lobby.Version, again.Lobby.Round, reopened.Lobby.Version)
	}
	// The game is over and the lobby waits, so the old game is still the
	// newest one; Carol, who played it, still reads it.
	if state := stack.state(room, carol); state.GameID != firstGame.GameID || state.Outcome == nil {
		t.Fatalf("Carol's state between games = %+v", state)
	}

	// Carol leaves, and the host seats a bot on her team. The bots in the
	// member settings survive the reopen, so the people only get ready again.
	stack.expect(stack.api.Delete(stack.lobbyPath(room)+"/membership", carol.header), http.StatusNoContent, nil)
	stack.expect(stack.api.Post(stack.lobbyPath(room)+"/seats", alice.header, map[string]any{"team_id": room.Teams[2].ID, "seat_settings": map[string]any{"bot_level": 1}}), http.StatusOK, nil)
	if bots := stack.bots(stack.snapshot(room, alice)); len(bots) != 2 || !sameBot(bots[alice.playerID], botChoice(models.LocalhostBot, 0)) || !sameBot(bots[bob.playerID], botChoice(models.LocalhostBot, 0)) {
		t.Fatalf("bots after the reopen = %+v", bots)
	}
	stack.expect(stack.api.Post(stack.lobbyPath(room)+"/start", alice.header, map[string]any{}), http.StatusConflict, nil)
	for _, member := range []guest{alice, bob} {
		stack.expect(stack.ready(room, member), http.StatusOK, nil)
	}

	second := start()
	secondGame := stack.state(room, alice)
	if second.Lobby.Round != 2 || secondGame.LobbyRound != 2 || secondGame.GameID == firstGame.GameID || secondGame.Outcome != nil || secondGame.Submitted {
		t.Fatalf("second start = round %d; Alice's state = %+v", second.Lobby.Round, secondGame)
	}
	newest, err := dal.FindLobbyGame(stack.database, room.Lobby.ID)
	if err != nil || newest.ID != secondGame.GameID || newest.LobbyRound != 2 {
		t.Fatalf("newest game = %+v, %v; want game %d", newest, err, secondGame.GameID)
	}
	// Carol played the first game, not the second: she can't read or play
	// the lobby's game any more.
	carolToken := stack.playerToken(carol, lobbyadapter.GameKey)
	for _, response := range []int{
		stack.api.Get("/game/from-lobby/"+room.Lobby.ID+"/generation", carolToken).Code,
		stack.api.Get("/game/from-lobby/"+room.Lobby.ID+"/replay", carolToken).Code,
		stack.api.Post("/game/from-lobby/"+room.Lobby.ID+"/deadline", carolToken, map[string]any{}).Code,
		stack.api.Post("/generation/commands", carolToken, map[string]any{"game_id": secondGame.GameID, "generation_id": secondGame.GenerationID, "request_id": uuid.NewString(), "commands": []any{}}).Code,
	} {
		if response != http.StatusNotFound {
			t.Fatalf("Carol's request to the second game = %d, want 404", response)
		}
	}
	reopen(bob, http.StatusConflict, &refusal)
	if refusal.Code != "game_running" {
		t.Fatalf("reopen during the second game = %+v", refusal)
	}
	// The lobby reached the game only through its signed hooks: the start
	// bound each round's game once, and the reopens asked whether a round
	// had ended.
	for _, hook := range gamehooks.Hooks {
		if stack.hookCalls[hook] == 0 {
			t.Errorf("the lobby never called %s", hook)
		}
	}
	if stack.hookCalls[gamehooks.HookStart] != 2 {
		t.Errorf("start was called %d times, want once per round", stack.hookCalls[gamehooks.HookStart])
	}
}

// The game of a round is over once it has an outcome. A round whose start
// bound no game counts as over, so its lobby can be reopened.
func TestLobbyRoundEnded(t *testing.T) {
	stack := newLobbyStack(t)
	lobbyID := uuid.NewString()
	for round, outcome := range map[int64]*models.GameOutcome{1: {Reason: models.GameOutcomeAllEliminated}, 2: nil} {
		game := models.Game{LobbyID: &lobbyID, LobbyRound: round, Outcome: outcome}
		if err := stack.database.Create(&game).Error; err != nil {
			t.Fatal(err)
		}
	}
	for round, want := range map[int64]bool{1: true, 2: false, 3: true} {
		if ended, err := dal.LobbyRoundEnded(stack.database, lobbyID, round); err != nil || ended != want {
			t.Fatalf("round %d ended = %v, %v; want %v", round, ended, err, want)
		}
	}
	newest, err := dal.FindLobbyGame(stack.database, lobbyID)
	if err != nil || newest.LobbyRound != 2 {
		t.Fatalf("newest game = %+v, %v; want round 2", newest, err)
	}
	// A lobby round has one game.
	duplicate := models.Game{LobbyID: &lobbyID, LobbyRound: 2}
	if err := stack.database.Create(&duplicate).Error; err == nil {
		t.Fatal("a second game of one lobby round was stored")
	}
}
