package handlers_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/humatest"
	"github.com/google/uuid"
	"islandBattles/backend/handlers"
	"islandBattles/backend/internal/testutils"
	"islandBattles/backend/models"
	"islandBattles/backend/services"
	"islandBattles/lobby/domain"
)

type commandAuthentication struct{ principal domain.Principal }

func (authentication commandAuthentication) CreateGuest(context.Context, domain.CreateGuestRequest) (domain.GuestSession, error) {
	return domain.GuestSession{}, domain.ErrUnauthenticated
}
func (authentication commandAuthentication) Authenticate(_ context.Context, authorization string) (domain.SessionVerification, error) {
	if authorization != "Bearer valid" {
		return domain.SessionVerification{}, domain.ErrUnauthenticated
	}
	return domain.SessionVerification{Principal: authentication.principal}, nil
}

func TestGenerationHTTPAuthenticationValidationAndStatus(t *testing.T) {
	database := testutils.InitTestDB(t)
	playerID, lobbyID, teamID := uuid.NewString(), uuid.NewString(), uuid.NewString()
	snapshot := domain.Snapshot{Lobby: domain.Lobby{ID: lobbyID, GameKey: "island-battles", Phase: domain.PhaseStarted}, Teams: []domain.Team{{ID: teamID, Order: 1}}, StartRoster: domain.StartRoster{{PlayerID: playerID, TeamID: teamID}}}
	publisher := &testutils.RecordedPublisher{}
	binding, err := services.RunLobbyGame(context.Background(), database, publisher, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	handler := handlers.LobbyGame{Database: database, Authenticator: commandAuthentication{domain.Principal{PlayerID: playerID}}, Publisher: publisher}
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
	if state.GameID != binding.GameID || len(state.Generation.Castles) != 1 || state.Generation.Castles[0].ID != 0 {
		t.Fatalf("state=%+v", state)
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
	stale := fmt.Sprintf(`{"game_id":%d,"generation_id":%d,"request_id":%q,"commands":[]}`, state.GameID, state.GenerationID, uuid.NewString())
	if response := api.Post("/generation/commands", "Authorization: Bearer valid", strings.NewReader(stale)); response.Code != 409 {
		t.Fatalf("stale=%d %s", response.Code, response.Body.String())
	}
}
