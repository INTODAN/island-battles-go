package handlers_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/INTODAN/mpl/domain"
	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/humatest"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"islandBattles/backend/dal"
	"islandBattles/backend/handlers"
	"islandBattles/backend/internal/testutils"
	"islandBattles/backend/models"
	"islandBattles/backend/services"
)

// resignGame starts a game of one player per bot kind, "manual" or
// "localhost", and serves the resign, deadline and batch routes.
func resignGame(t *testing.T, kinds ...string) (humatest.TestAPI, []string, string, *gorm.DB) {
	t.Helper()
	database := testutils.InitTestDB(t)
	lobbyID := uuid.NewString()
	snapshot := domain.Snapshot{Lobby: domain.Lobby{ID: lobbyID, GameKey: "island-battles", Phase: domain.PhaseStarted, GameSettings: testutils.CircleMapSettings(len(kinds), "")}}
	var players []string
	for index, kind := range kinds {
		player, team := uuid.NewString(), uuid.NewString()
		players = append(players, player)
		snapshot.Teams = append(snapshot.Teams, domain.Team{ID: team, Order: index + 1, Name: fmt.Sprintf("Team %d", index+1)})
		snapshot.StartRoster = append(snapshot.StartRoster, domain.StartRosterEntry{PlayerID: player, TeamID: team})
		snapshot.Members = append(snapshot.Members, domain.Member{PlayerID: player, DisplayName: fmt.Sprintf("Player %d", index+1), Kind: domain.MemberHuman,
			MemberSettings: json.RawMessage(fmt.Sprintf(`{"bot":{"kind":%q}}`, kind))})
	}
	publisher := &testutils.RecordedPublisher{}
	if _, err := services.RunLobbyGame(context.Background(), database, publisher, nil, snapshot); err != nil {
		t.Fatal(err)
	}
	handler := &handlers.LobbyGame{Database: database, Players: playerAuthentication{players}, Publisher: publisher}
	_, api := humatest.New(t)
	huma.Register(api, huma.Operation{OperationID: "testResign", Method: "POST", Path: "/game/from-lobby/{lobby_id}/resign"}, handler.Resign)
	huma.Register(api, huma.Operation{OperationID: "testDeadline", Method: "POST", Path: "/game/from-lobby/{lobby_id}/deadline"}, handler.ApplyTurnDeadline)
	huma.Register(api, huma.Operation{OperationID: "registerGenerationCommand", Method: "POST", Path: "/generation/commands", SkipValidateBody: true}, handler.RegisterGenerationCommands)
	return api, players, lobbyID, database
}

// endOpenTurn moves the open generation's creation back, so that its turn's
// time has ended: the handlers run on the real clock.
func endOpenTurn(t *testing.T, database *gorm.DB, lobbyID string) {
	t.Helper()
	game, err := dal.FindLobbyGame(database, lobbyID)
	if err != nil {
		t.Fatal(err)
	}
	generation, err := dal.GetLatestGenerationForGame(database, game.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.Model(&models.Generation{}).Where(&models.Generation{Model: gorm.Model{ID: generation.ID}}).Update("created_at", time.Now().Add(-2*time.Second)).Error; err != nil {
		t.Fatal(err)
	}
}

func TestResignHTTP(t *testing.T) {
	api, players, lobbyID, database := resignGame(t, "manual", "manual", "manual")
	path := "/game/from-lobby/" + lobbyID + "/resign"
	bearer := func(player string) string { return "Authorization: Bearer " + player }
	expect := func(code int, body []byte, status int, wantType string) {
		t.Helper()
		var problem huma.ErrorModel
		if err := json.Unmarshal(body, &problem); err != nil {
			t.Fatal(err)
		}
		if code != status || problem.Type != wantType {
			t.Fatalf("answer = %d %s, want %d %s", code, body, status, wantType)
		}
	}

	// Only a player token of a roster player resigns: not a missing token,
	// not a submit token, not another lobby's player.
	if response := api.Post(path); response.Code != 401 {
		t.Fatalf("unauthenticated resign=%d", response.Code)
	}
	if response := api.Post(path, "Authorization: Bearer ibst1.e30.c2ln"); response.Code != 401 {
		t.Fatalf("resign with a submit token=%d", response.Code)
	}
	if response := api.Post("/game/from-lobby/"+uuid.NewString()+"/resign", bearer(players[1])); response.Code != 404 {
		t.Fatalf("resign of another lobby=%d", response.Code)
	}

	// The answer is the caller's state; the team is still on the board
	// until the turn plays. A repeat changes nothing.
	var state models.GenerationState
	for range 2 {
		response := api.Post(path, bearer(players[1]))
		if response.Code != 200 {
			t.Fatalf("resign=%d %s", response.Code, response.Body.String())
		}
		if err := json.Unmarshal(response.Body.Bytes(), &state); err != nil {
			t.Fatal(err)
		}
		if state.TeamColor != 2 || state.TurnNumber != 1 || state.Outcome != nil || len(state.Departures) != 0 {
			t.Fatalf("resign state=%+v", state)
		}
	}

	// Once the turn has played, the team is out.
	endOpenTurn(t, database, lobbyID)
	response := api.Post("/game/from-lobby/"+lobbyID+"/deadline", bearer(players[0]))
	if err := json.Unmarshal(response.Body.Bytes(), &state); err != nil || response.Code != 200 {
		t.Fatalf("deadline=%d %s", response.Code, response.Body.String())
	}
	if state.TurnNumber != 2 || state.Outcome != nil || len(state.Departures) != 1 || state.Departures[0].TeamColor != 2 || state.Departures[0].Kind != models.DepartureResigned ||
		state.Departures[0].TurnNumber == nil || *state.Departures[0].TurnNumber != 2 {
		t.Fatalf("state after the turn=%+v", state)
	}
	response = api.Post(path, bearer(players[1]))
	expect(response.Code, response.Body.Bytes(), 409, services.TeamOutProblemType)
	batch := fmt.Sprintf(`{"game_id":%d,"generation_id":%d,"request_id":%q,"commands":[],"complete":false}`, state.GameID, state.GenerationID, uuid.NewString())
	response = api.Post("/generation/commands", bearer(players[1]), strings.NewReader(batch))
	expect(response.Code, response.Body.Bytes(), 409, services.TeamOutProblemType)

	// After the outcome, a resignation is refused as every command is.
	if response := api.Post(path, bearer(players[0])); response.Code != 200 {
		t.Fatalf("second team's resign=%d %s", response.Code, response.Body.String())
	}
	endOpenTurn(t, database, lobbyID)
	response = api.Post("/game/from-lobby/"+lobbyID+"/deadline", bearer(players[2]))
	if err := json.Unmarshal(response.Body.Bytes(), &state); err != nil || response.Code != 200 || state.Outcome == nil || state.Outcome.Reason != models.GameOutcomeLastTeamStanding {
		t.Fatalf("deadline=%d %s", response.Code, response.Body.String())
	}
	response = api.Post(path, bearer(players[2]))
	expect(response.Code, response.Body.Bytes(), 409, services.GameEndedProblemType)

	// A game that waits for every player can't be resigned.
	api, players, lobbyID, _ = resignGame(t, "localhost", "localhost")
	response = api.Post("/game/from-lobby/"+lobbyID+"/resign", bearer(players[0]))
	expect(response.Code, response.Body.Bytes(), 409, services.NotRealTimeProblemType)
}
