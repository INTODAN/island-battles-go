package handlers_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/INTODAN/mpl/domain"
	"github.com/INTODAN/mpl/playertoken"
	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/humatest"
	"github.com/google/uuid"
	"islandBattles/backend/handlers"
	"islandBattles/backend/internal/testutils"
	"islandBattles/backend/models"
	"islandBattles/backend/services"
)

type commandAuthentication struct{ principal domain.Principal }

func (authentication commandAuthentication) AuthenticatePlayer(_ context.Context, authorization string) (domain.Principal, error) {
	if authorization != "Bearer valid" {
		return domain.Principal{}, playertoken.ErrInvalid
	}
	return authentication.principal, nil
}

func TestGenerationHTTPAuthenticationValidationAndStatus(t *testing.T) {
	database := testutils.InitTestDB(t)
	playerID, lobbyID, teamID := uuid.NewString(), uuid.NewString(), uuid.NewString()
	snapshot := domain.Snapshot{Lobby: domain.Lobby{ID: lobbyID, GameKey: "island-battles", Phase: domain.PhaseStarted}, Teams: []domain.Team{{ID: teamID, Order: 1}}, StartRoster: domain.StartRoster{{PlayerID: playerID, TeamID: teamID}}}
	publisher := &testutils.RecordedPublisher{}
	binding, err := services.RunLobbyGame(context.Background(), database, publisher, nil, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	handler := handlers.LobbyGame{Database: database, Players: commandAuthentication{domain.Principal{PlayerID: playerID}}, Publisher: publisher}
	_, api := humatest.New(t)
	huma.Register(api, huma.Operation{OperationID: "testCurrent", Method: "GET", Path: "/game/from-lobby/{lobby_id}/generation"}, handler.CurrentGeneration)
	huma.Register(api, huma.Operation{OperationID: "registerGenerationCommand", Method: "POST", Path: "/generation/commands"}, handler.RegisterGenerationCommands)
	path := "/game/from-lobby/" + lobbyID + "/generation"
	if response := api.Get(path); response.Code != 401 {
		t.Fatalf("unauthenticated read=%d", response.Code)
	}
	response := api.Get(path, "Authorization: Bearer valid")
	if response.Code != 200 {
		t.Fatalf("read=%d %s", response.Code, response.Body.String())
	}
	var state models.GenerationState
	if err := json.Unmarshal(response.Body.Bytes(), &state); err != nil {
		t.Fatal(err)
	}
	if state.GameID != binding.GameID || len(state.Generation.Castles) != 1 || state.Generation.Castles[0].ID != 0 || state.TurnDeadlineAt == nil {
		t.Fatalf("state=%+v", state)
	}
	// Before the deadline, applying it answers the state and changes nothing.
	huma.Register(api, huma.Operation{OperationID: "testDeadline", Method: "POST", Path: "/game/from-lobby/{lobby_id}/deadline"}, handler.ApplyTurnDeadline)
	deadlinePath := "/game/from-lobby/" + lobbyID + "/deadline"
	if response := api.Post(deadlinePath); response.Code != 401 {
		t.Fatalf("unauthenticated deadline=%d", response.Code)
	}
	if response := api.Post("/game/from-lobby/"+uuid.NewString()+"/deadline", "Authorization: Bearer valid"); response.Code != 404 {
		t.Fatalf("deadline of another lobby=%d", response.Code)
	}
	response = api.Post(deadlinePath, "Authorization: Bearer valid")
	var applied models.GenerationState
	if err := json.Unmarshal(response.Body.Bytes(), &applied); err != nil || response.Code != 200 {
		t.Fatalf("deadline=%d %s", response.Code, response.Body.String())
	}
	if applied.GenerationID != state.GenerationID || applied.Outcome != nil || !applied.TurnDeadlineAt.Equal(*state.TurnDeadlineAt) {
		t.Fatalf("deadline state=%+v", applied)
	}
	request := fmt.Sprintf(`{"game_id":%d,"generation_id":%d,"request_id":%q,"commands":[]}`, state.GameID, state.GenerationID, uuid.NewString())
	if response := api.Post("/generation/commands", strings.NewReader(request)); response.Code != 401 {
		t.Fatalf("unauthenticated write=%d %s", response.Code, response.Body.String())
	}
	malformed := fmt.Sprintf(`{"game_id":%d,"generation_id":%d,"request_id":%q,"commands":[{"command_type":"send_soldier_group","parameters":{"soldier_amount":1}}]}`, state.GameID, state.GenerationID, uuid.NewString())
	if response := api.Post("/generation/commands", "Authorization: Bearer valid", strings.NewReader(malformed)); response.Code != 422 {
		t.Fatalf("invalid=%d %s", response.Code, response.Body.String())
	}
	forbidden := fmt.Sprintf(`{"game_id":%d,"generation_id":%d,"request_id":%q,"commands":[{"command_type":"move_soldier_groups","parameters":{}}]}`, state.GameID, state.GenerationID, uuid.NewString())
	if response := api.Post("/generation/commands", "Authorization: Bearer valid", strings.NewReader(forbidden)); response.Code != 403 {
		t.Fatalf("forbidden=%d %s", response.Code, response.Body.String())
	}
	response = api.Post("/generation/commands", "Authorization: Bearer valid", strings.NewReader(request))
	if response.Code != 200 {
		t.Fatalf("empty complete=%d %s", response.Code, response.Body.String())
	}
	var next models.GenerationState
	if err := json.Unmarshal(response.Body.Bytes(), &next); err != nil {
		t.Fatal(err)
	}
	if next.GenerationID == state.GenerationID || next.Submitted {
		t.Fatalf("omitted complete did not advance: %+v", next)
	}
	if response := api.Post("/generation/commands", "Authorization: Bearer valid", strings.NewReader(request)); response.Code != 200 {
		t.Fatalf("retry=%d %s", response.Code, response.Body.String())
	}
	// The only team is the last team standing, so its first turn ends the game.
	if next.Outcome == nil || next.Outcome.Reason != models.GameOutcomeLastTeamStanding {
		t.Fatalf("a one-team game did not end: %+v", next.Outcome)
	}
	stale := fmt.Sprintf(`{"game_id":%d,"generation_id":%d,"request_id":%q,"commands":[]}`, state.GameID, next.GenerationID, uuid.NewString())
	response = api.Post("/generation/commands", "Authorization: Bearer valid", strings.NewReader(stale))
	var problem huma.ErrorModel
	if err := json.Unmarshal(response.Body.Bytes(), &problem); err != nil {
		t.Fatal(err)
	}
	if response.Code != 409 || problem.Type != services.GameEndedProblemType || !strings.HasPrefix(problem.Detail, "the game has ended: ") {
		t.Fatalf("ended=%d %s", response.Code, response.Body.String())
	}
	if response := api.Get(path, "Authorization: Bearer valid"); response.Code != 200 || !strings.Contains(response.Body.String(), `"outcome":{"reason":"last_team_standing"`) {
		t.Fatalf("finished read=%d %s", response.Code, response.Body.String())
	}
}

func TestGenerationHTTPCarriesBotMemory(t *testing.T) {
	database := testutils.InitTestDB(t)
	players := []string{uuid.NewString(), uuid.NewString()}
	lobbyID, blue, red := uuid.NewString(), uuid.NewString(), uuid.NewString()
	snapshot := domain.Snapshot{Lobby: domain.Lobby{ID: lobbyID, GameKey: "island-battles", Phase: domain.PhaseStarted}, Teams: []domain.Team{{ID: blue, Order: 1}, {ID: red, Order: 2}},
		StartRoster: domain.StartRoster{{PlayerID: players[0], TeamID: blue}, {PlayerID: players[1], TeamID: red}}}
	publisher := &testutils.RecordedPublisher{}
	if _, err := services.RunLobbyGame(context.Background(), database, publisher, nil, snapshot); err != nil {
		t.Fatal(err)
	}
	handler := handlers.LobbyGame{Database: database, Players: commandAuthentication{domain.Principal{PlayerID: players[0]}}, Publisher: publisher}
	_, api := humatest.New(t)
	huma.Register(api, huma.Operation{OperationID: "testCurrent", Method: "GET", Path: "/game/from-lobby/{lobby_id}/generation"}, handler.CurrentGeneration)
	huma.Register(api, huma.Operation{OperationID: "registerGenerationCommand", Method: "POST", Path: "/generation/commands", SkipValidateBody: true}, handler.RegisterGenerationCommands)
	path := "/game/from-lobby/" + lobbyID + "/generation"
	response := api.Get(path, "Authorization: Bearer valid")
	if response.Code != 200 || !strings.Contains(response.Body.String(), `"memory":null`) {
		t.Fatalf("first read=%d %s", response.Code, response.Body.String())
	}
	var state models.GenerationState
	if err := json.Unmarshal(response.Body.Bytes(), &state); err != nil {
		t.Fatal(err)
	}

	tooLarge := fmt.Sprintf(`{"game_id":%d,"generation_id":%d,"request_id":%q,"commands":[],"memory":{"x":%q}}`, state.GameID, state.GenerationID, uuid.NewString(), strings.Repeat("a", models.MaxBotMemoryBytes))
	response = api.Post("/generation/commands", "Authorization: Bearer valid", strings.NewReader(tooLarge))
	var problem huma.ErrorModel
	if err := json.Unmarshal(response.Body.Bytes(), &problem); err != nil {
		t.Fatal(err)
	}
	if response.Code != 422 || problem.Type != services.BotMemoryTooLargeProblemType {
		t.Fatalf("too large=%d %s", response.Code, response.Body.String())
	}

	// The memory's digits come back as they were sent.
	request := fmt.Sprintf(`{"game_id":%d,"generation_id":%d,"request_id":%q,"commands":[],"memory":{"big":12345678901234567890,"plan":[1,2]}}`, state.GameID, state.GenerationID, uuid.NewString())
	response = api.Post("/generation/commands", "Authorization: Bearer valid", strings.NewReader(request))
	if response.Code != 200 || !strings.Contains(response.Body.String(), `"memory":{"big":12345678901234567890,"plan":[1,2]}`) {
		t.Fatalf("complete=%d %s", response.Code, response.Body.String())
	}
	response = api.Get(path, "Authorization: Bearer valid")
	if response.Code != 200 || !strings.Contains(response.Body.String(), `"big":12345678901234567890`) || !strings.Contains(response.Body.String(), `"plan":[1,2]`) {
		t.Fatalf("read=%d %s", response.Code, response.Body.String())
	}
}
