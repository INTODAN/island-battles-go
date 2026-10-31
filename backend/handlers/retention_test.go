package handlers_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/humatest"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"islandBattles/backend/dal"
	"islandBattles/backend/handlers"
	"islandBattles/backend/internal/testutils"
	"islandBattles/backend/models"
	"islandBattles/backend/services"
	"islandBattles/lobby/domain"
)

// countingRetention counts the passes it was asked for, and records how many
// generations the game had at each, which shows whether a turn had committed.
type countingRetention struct {
	database    *gorm.DB
	gameID      uint
	generations []int64
}

func (retention *countingRetention) RunIfDue(context.Context) {
	count, err := dal.CountGenerationsForGame(retention.database, retention.gameID)
	if err != nil {
		count = -1
	}
	retention.generations = append(retention.generations, count)
}

func TestGameRequestsEndWithARetentionPass(t *testing.T) {
	database := testutils.InitTestDB(t)
	playerID, lobbyID, teamID := uuid.NewString(), uuid.NewString(), uuid.NewString()
	binding, err := services.RunLobbyGame(context.Background(), database, &testutils.RecordedPublisher{}, domain.Snapshot{
		Lobby: domain.Lobby{ID: lobbyID, GameKey: "island-battles", Phase: domain.PhaseStarted},
		Teams: []domain.Team{{ID: teamID, Order: 1}}, StartRoster: domain.StartRoster{{PlayerID: playerID, TeamID: teamID}},
	})
	if err != nil {
		t.Fatal(err)
	}
	retention := &countingRetention{database: database, gameID: binding.GameID}
	handler := handlers.LobbyGame{Database: database, Authenticator: commandAuthentication{domain.Principal{PlayerID: playerID}},
		Publisher: &testutils.RecordedPublisher{}, Retention: retention}
	_, api := humatest.New(t)
	huma.Register(api, huma.Operation{OperationID: "current", Method: http.MethodGet, Path: "/game/from-lobby/{lobby_id}/generation"}, handler.CurrentGeneration)
	huma.Register(api, huma.Operation{OperationID: "commands", Method: http.MethodPost, Path: "/generation/commands"}, handler.RegisterGenerationCommands)
	huma.Register(api, huma.Operation{OperationID: "replay", Method: http.MethodGet, Path: "/game/from-lobby/{lobby_id}/replay"}, handler.Replay)
	huma.Register(api, huma.Operation{OperationID: "logs", Method: http.MethodGet, Path: "/game/from-lobby/{lobby_id}/logs"}, handler.GenerationLogs)
	huma.Register(api, huma.Operation{OperationID: "demo", Method: http.MethodGet, Path: "/game/from-lobby/{lobby_id}/demo"}, handler.Demo)
	statePath, replayPath := "/game/from-lobby/"+lobbyID+"/generation", "/game/from-lobby/"+lobbyID+"/replay"
	expectPasses := func(step string, want int) {
		t.Helper()
		if len(retention.generations) != want {
			t.Fatalf("%s: %d passes, want %d", step, len(retention.generations), want)
		}
	}

	response := api.Get(statePath, "Authorization: Bearer valid")
	if response.Code != http.StatusOK {
		t.Fatalf("read=%d %s", response.Code, response.Body.String())
	}
	expectPasses("state read", 1)
	var state models.GenerationState
	if err := json.Unmarshal(response.Body.Bytes(), &state); err != nil {
		t.Fatal(err)
	}
	turn := fmt.Sprintf(`{"game_id":%d,"generation_id":%d,"request_id":%q,"commands":[]}`, state.GameID, state.GenerationID, uuid.NewString())
	if response := api.Post("/generation/commands", "Authorization: Bearer valid", strings.NewReader(turn)); response.Code != http.StatusOK {
		t.Fatalf("turn=%d %s", response.Code, response.Body.String())
	}
	expectPasses("turn", 2)
	// The only roster player completed the turn, so the pass ran after the
	// turn's new generation had committed.
	if retention.generations[1] != 2 {
		t.Fatalf("the turn's pass saw %d generations, want 2", retention.generations[1])
	}
	if response := api.Get(replayPath, "Authorization: Bearer valid"); response.Code != http.StatusOK {
		t.Fatalf("replay=%d %s", response.Code, response.Body.String())
	}
	expectPasses("replay", 3)

	// Failed requests end with a pass too.
	if response := api.Get(statePath); response.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated read=%d", response.Code)
	}
	expectPasses("unauthenticated state read", 4)
	stale := fmt.Sprintf(`{"game_id":%d,"generation_id":%d,"request_id":%q,"commands":[]}`, state.GameID, state.GenerationID, uuid.NewString())
	if response := api.Post("/generation/commands", "Authorization: Bearer valid", strings.NewReader(stale)); response.Code != http.StatusConflict {
		t.Fatalf("stale turn=%d %s", response.Code, response.Body.String())
	}
	expectPasses("rejected turn", 5)
	if response := api.Get("/game/from-lobby/"+uuid.NewString()+"/replay", "Authorization: Bearer valid"); response.Code != http.StatusNotFound {
		t.Fatalf("unknown replay=%d", response.Code)
	}
	expectPasses("unknown replay", 6)
	logsPath := fmt.Sprintf("/game/from-lobby/%s/logs?generation_id=%d", lobbyID, state.GenerationID)
	if response := api.Get(logsPath, "Authorization: Bearer valid"); response.Code != http.StatusOK {
		t.Fatalf("logs=%d %s", response.Code, response.Body.String())
	}
	expectPasses("logs", 7)
	if response := api.Get("/game/from-lobby/"+lobbyID+"/demo", "Authorization: Bearer valid"); response.Code != http.StatusOK || !json.Valid(response.Body.Bytes()) {
		t.Fatalf("demo=%d %s", response.Code, response.Body.String())
	}
	expectPasses("demo", 8)
	if response := api.Get("/game/from-lobby/"+uuid.NewString()+"/demo", "Authorization: Bearer valid"); response.Code != http.StatusNotFound {
		t.Fatalf("unknown demo=%d", response.Code)
	}
	expectPasses("unknown demo", 9)
}
