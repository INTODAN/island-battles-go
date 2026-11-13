package handlers_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/humatest"
	"github.com/google/uuid"
	"islandBattles/backend/handlers"
	"islandBattles/backend/internal/testutils"
	"islandBattles/backend/models"
	"islandBattles/backend/services"
	"islandBattles/backend/submittoken"
	"islandBattles/lobby/domain"
	"islandBattles/lobby/playertoken"
)

// playerAuthentication accepts "Bearer <player ID>" for each of its players.
type playerAuthentication struct{ players []string }

func (authentication playerAuthentication) AuthenticatePlayer(_ context.Context, authorization string) (domain.Principal, error) {
	for _, player := range authentication.players {
		if authorization == "Bearer "+player {
			return domain.Principal{PlayerID: player}, nil
		}
	}
	return domain.Principal{}, playertoken.ErrInvalid
}

// deliveryGame starts a two-team game and serves the turn delivery and the
// command batch, as the routes do.
func deliveryGame(t *testing.T) (humatest.TestAPI, *submittoken.Signer, []string, string, *handlers.LobbyGame) {
	t.Helper()
	database := testutils.InitTestDB(t)
	players := []string{uuid.NewString(), uuid.NewString()}
	lobbyID, blue, red := uuid.NewString(), uuid.NewString(), uuid.NewString()
	snapshot := domain.Snapshot{Lobby: domain.Lobby{ID: lobbyID, GameKey: "island-battles", Phase: domain.PhaseStarted}, Teams: []domain.Team{{ID: blue, Order: 1}, {ID: red, Order: 2}},
		StartRoster: domain.StartRoster{{PlayerID: players[0], TeamID: blue}, {PlayerID: players[1], TeamID: red}}}
	publisher := &testutils.RecordedPublisher{}
	if _, err := services.RunLobbyGame(context.Background(), database, publisher, snapshot); err != nil {
		t.Fatal(err)
	}
	signer, err := submittoken.New([]byte("test-submit-token-secret"))
	if err != nil {
		t.Fatal(err)
	}
	handler := &handlers.LobbyGame{Database: database, Players: playerAuthentication{players}, Publisher: publisher, SubmitTokens: signer}
	_, api := humatest.New(t)
	huma.Register(api, huma.Operation{OperationID: "testDelivery", Method: "GET", Path: "/game/from-lobby/{lobby_id}/turn-delivery"}, handler.TurnDelivery)
	huma.Register(api, huma.Operation{OperationID: "registerGenerationCommand", Method: "POST", Path: "/generation/commands", SkipValidateBody: true}, handler.RegisterGenerationCommands)
	return api, signer, players, lobbyID, handler
}

func readDelivery(t *testing.T, api humatest.TestAPI, lobbyID, player string) models.TurnDelivery {
	t.Helper()
	response := api.Get("/game/from-lobby/"+lobbyID+"/turn-delivery", "Authorization: Bearer "+player, "Host: example.com")
	if response.Code != 200 {
		t.Fatalf("delivery=%d %s", response.Code, response.Body.String())
	}
	var delivery models.TurnDelivery
	if err := json.Unmarshal(response.Body.Bytes(), &delivery); err != nil {
		t.Fatal(err)
	}
	return delivery
}

func batch(gameID, generationID uint, memory string) string {
	return fmt.Sprintf(`{"game_id":%d,"generation_id":%d,"request_id":%q,"commands":[],"memory":%s,"logs":[{"level":"info","logger":"island_battles.player","message":"hi"}],"complete":true}`, gameID, generationID, uuid.NewString(), memory)
}

func TestTurnDeliveryIsSubmittedWithItsToken(t *testing.T) {
	api, signer, players, lobbyID, handler := deliveryGame(t)
	path := "/game/from-lobby/" + lobbyID + "/turn-delivery"
	if response := api.Get(path); response.Code != 401 {
		t.Fatalf("unauthenticated delivery=%d", response.Code)
	}
	if response := api.Get("/game/from-lobby/"+uuid.NewString()+"/turn-delivery", "Authorization: Bearer "+players[0]); response.Code != 404 {
		t.Fatalf("another lobby's delivery=%d", response.Code)
	}
	delivery := readDelivery(t, api, lobbyID, players[0])
	turn := delivery.Turn
	if turn.GameID == 0 || turn.TeamColor != 1 || turn.Generation.ID == 0 || len(turn.Upgrades) == 0 || turn.Memory != nil || turn.TurnDeadlineAt == nil {
		t.Fatalf("turn=%+v", turn)
	}
	// The request's own address, without a public URL.
	if delivery.Submit.URL != "http://example.com/generation/commands" || delivery.Submit.DeadlineAt == nil || !delivery.Submit.DeadlineAt.Equal(*turn.TurnDeadlineAt) {
		t.Fatalf("submit=%+v", delivery.Submit)
	}
	claims, err := signer.Verify(delivery.Submit.Token, time.Now())
	if err != nil || claims.GameID != turn.GameID || claims.PlayerID != players[0] || claims.GenerationID != turn.Generation.ID ||
		!claims.Expires.Equal(turn.TurnDeadlineAt.Add(30*time.Second).Truncate(time.Second)) {
		t.Fatalf("claims=%+v, %v", claims, err)
	}
	handler.PublicURL = "https://island-battles.example.org"
	if url := readDelivery(t, api, lobbyID, players[0]).Submit.URL; url != "https://island-battles.example.org/generation/commands" {
		t.Fatalf("url=%s", url)
	}

	token := "Authorization: Bearer " + delivery.Submit.Token
	other, _ := submittoken.New([]byte("another secret"))
	issue := func(signer *submittoken.Signer, claims submittoken.Claims) string {
		token, err := signer.Issue(claims)
		if err != nil {
			t.Fatal(err)
		}
		return "Authorization: Bearer " + token
	}
	valid := submittoken.Claims{GameID: turn.GameID, PlayerID: players[0], GenerationID: turn.Generation.ID, Expires: time.Now().Add(time.Minute)}
	wrongPlayer, expired := valid, valid
	wrongPlayer.PlayerID = uuid.NewString()
	expired.Expires = time.Now().Add(-time.Second)
	for name, attempt := range map[string]struct{ header, body string }{
		"another generation":  {token, batch(turn.GameID, turn.Generation.ID+1, "null")},
		"another game":        {token, batch(turn.GameID+1, turn.Generation.ID, "null")},
		"a player not in it":  {issue(signer, wrongPlayer), batch(turn.GameID, turn.Generation.ID, "null")},
		"an expired token":    {issue(signer, expired), batch(turn.GameID, turn.Generation.ID, "null")},
		"another secret":      {issue(other, valid), batch(turn.GameID, turn.Generation.ID, "null")},
		"an altered token":    {token + "x", batch(turn.GameID, turn.Generation.ID, "null")},
		"no signer to verify": {token, batch(turn.GameID, turn.Generation.ID, "null")},
	} {
		if name == "no signer to verify" {
			handler.SubmitTokens = nil
		}
		response := api.Post("/generation/commands", attempt.header, strings.NewReader(attempt.body))
		handler.SubmitTokens = signer
		if response.Code != 401 {
			t.Errorf("%s: %d %s", name, response.Code, response.Body.String())
		}
	}

	// The token completes the player's turn, with its memory and logs, as
	// the player's session would.
	response := api.Post("/generation/commands", token, strings.NewReader(batch(turn.GameID, turn.Generation.ID, `{"plan":1}`)))
	if response.Code != 200 {
		t.Fatalf("submit=%d %s", response.Code, response.Body.String())
	}
	var state models.GenerationState
	if err := json.Unmarshal(response.Body.Bytes(), &state); err != nil {
		t.Fatal(err)
	}
	if state.PlayerID != players[0] || !state.Submitted || state.GenerationID != turn.Generation.ID || string(state.Memory) != `{"plan":1}` || state.LogLinesStored != 1 {
		t.Fatalf("state=%+v", state)
	}
	// Nothing is left to play for this turn.
	response = api.Get(path, "Authorization: Bearer "+players[0])
	if response.Code != 409 || !strings.Contains(response.Body.String(), "already complete") {
		t.Fatalf("completed delivery=%d %s", response.Code, response.Body.String())
	}
	// The other player's token plays the turn on, and the next delivery
	// carries the stored memory.
	second := readDelivery(t, api, lobbyID, players[1])
	if second.Turn.TeamColor != 2 {
		t.Fatalf("second=%+v", second.Turn)
	}
	if response := api.Post("/generation/commands", "Authorization: Bearer "+second.Submit.Token, strings.NewReader(batch(turn.GameID, turn.Generation.ID, "null"))); response.Code != 200 {
		t.Fatalf("second submit=%d %s", response.Code, response.Body.String())
	}
	next := readDelivery(t, api, lobbyID, players[0])
	if next.Turn.Generation.ID == turn.Generation.ID || string(next.Turn.Memory) != `{"plan":1}` {
		t.Fatalf("next=%+v", next.Turn)
	}
	// The first turn's token doesn't submit the next turn.
	if response := api.Post("/generation/commands", token, strings.NewReader(batch(turn.GameID, next.Turn.Generation.ID, "null"))); response.Code != 401 {
		t.Fatalf("an old token=%d %s", response.Code, response.Body.String())
	}
}

func TestTurnDeliveryRefusesWhenThereIsNothingToPlay(t *testing.T) {
	api, _, players, lobbyID, handler := deliveryGame(t)
	delivery := readDelivery(t, api, lobbyID, players[0])
	// Red loses its castles: its player has no turn to play.
	generation := delivery.Turn.Generation
	result := handler.Database.Model(&models.Castle{}).Where("map_id = (SELECT map_id FROM generations WHERE id = ?) AND team_color = ?", generation.ID, 2).Update("team_color", 0)
	if result.Error != nil || result.RowsAffected == 0 {
		t.Fatalf("update=%v, %d rows", result.Error, result.RowsAffected)
	}
	response := api.Get("/game/from-lobby/"+lobbyID+"/turn-delivery", "Authorization: Bearer "+players[1])
	if response.Code != 409 || !strings.Contains(response.Body.String(), "eliminated") {
		t.Fatalf("eliminated=%d %s", response.Code, response.Body.String())
	}
	// Blue's turn ends the game: Blue is the last team standing.
	if response := api.Post("/generation/commands", "Authorization: Bearer "+delivery.Submit.Token, strings.NewReader(batch(delivery.Turn.GameID, generation.ID, "null"))); response.Code != 200 {
		t.Fatalf("submit=%d %s", response.Code, response.Body.String())
	}
	response = api.Get("/game/from-lobby/"+lobbyID+"/turn-delivery", "Authorization: Bearer "+players[0])
	var problem huma.ErrorModel
	if err := json.Unmarshal(response.Body.Bytes(), &problem); err != nil {
		t.Fatal(err)
	}
	if response.Code != 409 || problem.Type != services.GameEndedProblemType {
		t.Fatalf("ended=%d %s", response.Code, response.Body.String())
	}
}

func TestTurnDeliveryEndsAnOverdueTurn(t *testing.T) {
	api, _, players, lobbyID, handler := deliveryGame(t)
	delivery := readDelivery(t, api, lobbyID, players[0])
	// The turn's deadline passed long ago.
	if err := handler.Database.Exec("UPDATE generations SET created_at = created_at - interval '1 hour' WHERE id = ?", delivery.Turn.Generation.ID).Error; err != nil {
		t.Fatal(err)
	}
	response := api.Get("/game/from-lobby/"+lobbyID+"/turn-delivery", "Authorization: Bearer "+players[0])
	if response.Code != 409 || !strings.Contains(response.Body.String(), services.GameEndedProblemType) {
		t.Fatalf("overdue=%d %s", response.Code, response.Body.String())
	}
}
