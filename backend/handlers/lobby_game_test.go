package handlers_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
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

// bearerPlayers maps whole Authorization headers to players.
type bearerPlayers map[string]domain.Principal

func (players bearerPlayers) CreateGuest(context.Context, domain.CreateGuestRequest) (domain.GuestSession, error) {
	return domain.GuestSession{}, domain.ErrUnauthenticated
}
func (players bearerPlayers) Authenticate(_ context.Context, authorization string) (domain.SessionVerification, error) {
	principal, exists := players[authorization]
	if !exists {
		return domain.SessionVerification{}, domain.ErrUnauthenticated
	}
	return domain.SessionVerification{Principal: principal}, nil
}

// dropLobbyTables stands in for deleting the lobby. Authentication is faked
// here, so a game request that still read any lobby table would fail.
func dropLobbyTables(t *testing.T, database *gorm.DB) {
	t.Helper()
	tables, err := database.Migrator().GetTables()
	if err != nil {
		t.Fatal(err)
	}
	var lobbyTables []any
	for _, table := range tables {
		if strings.HasPrefix(table, "lobby_") {
			lobbyTables = append(lobbyTables, table)
		}
	}
	if len(lobbyTables) == 0 {
		t.Fatal("found no lobby tables to drop")
	}
	if err := database.Migrator().DropTable(lobbyTables...); err != nil {
		t.Fatal(err)
	}
}

func TestStartedGameServesItsRosterWithoutTheLobby(t *testing.T) {
	database := testutils.InitTestDB(t)
	alice, bob, lobbyID, red, blue := uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString()
	snapshot := domain.Snapshot{
		Lobby:       domain.Lobby{ID: lobbyID, GameKey: "island-battles", Phase: domain.PhaseStarted},
		Teams:       []domain.Team{{ID: red, Name: "Red", Order: 1}, {ID: blue, Name: "Blue", Order: 2}},
		StartRoster: domain.StartRoster{{PlayerID: alice, TeamID: red, TeamOrder: 1}, {PlayerID: bob, TeamID: blue, TeamOrder: 2}},
		Members:     []domain.Member{{PlayerID: alice, DisplayName: "Alice"}, {PlayerID: bob, DisplayName: "Bob"}},
	}
	// Binding at start is the only time the game reads its lobby.
	if _, err := services.RunLobbyGame(database, snapshot); err != nil {
		t.Fatal(err)
	}
	dropLobbyTables(t, database)
	handler := handlers.LobbyGame{Database: database, Authenticator: bearerPlayers{
		"Bearer alice": {PlayerID: alice}, "Bearer bob": {PlayerID: bob}, "Bearer outsider": {PlayerID: uuid.NewString()},
	}}
	_, api := humatest.New(t)
	huma.Register(api, huma.Operation{OperationID: "replay", Method: http.MethodGet, Path: "/game/from-lobby/{lobby_id}/replay"}, handler.Replay)
	huma.Register(api, huma.Operation{OperationID: "current", Method: http.MethodGet, Path: "/game/from-lobby/{lobby_id}/generation"}, handler.CurrentGeneration)
	huma.Register(api, huma.Operation{OperationID: "commands", Method: http.MethodPost, Path: "/generation/commands"}, handler.RegisterGenerationCommands)
	statePath, replayPath := "/game/from-lobby/"+lobbyID+"/generation", "/game/from-lobby/"+lobbyID+"/replay"

	response := api.Get(statePath, "Authorization: Bearer alice")
	if response.Code != http.StatusOK {
		t.Fatalf("roster read=%d %s", response.Code, response.Body.String())
	}
	var initial models.GenerationState
	if err := json.Unmarshal(response.Body.Bytes(), &initial); err != nil {
		t.Fatal(err)
	}
	if initial.PlayerID != alice || initial.Channel != "player:"+alice {
		t.Fatalf("state=%+v", initial)
	}
	var origin, target uint
	for _, castle := range initial.Generation.Castles {
		if castle.TeamColor == initial.TeamColor {
			origin = castle.ID
		} else {
			target = castle.ID
		}
	}
	commands := func(credential, body string) *httptest.ResponseRecorder {
		request := fmt.Sprintf(`{"game_id":%d,"generation_id":%d,"request_id":%q,"commands":[%s]}`, initial.GameID, initial.GenerationID, uuid.NewString(), body)
		return api.Post("/generation/commands", "Authorization: "+credential, strings.NewReader(request))
	}
	// Callers off the roster are rejected, and unknown credentials stay unauthenticated.
	for credential, want := range map[string]int{"Bearer outsider": http.StatusNotFound, "Bearer stranger": http.StatusUnauthorized} {
		if response := api.Get(statePath, "Authorization: "+credential); response.Code != want {
			t.Fatalf("%s read=%d", credential, response.Code)
		}
		if response := commands(credential, ""); response.Code != want {
			t.Fatalf("%s commands=%d", credential, response.Code)
		}
		if response := api.Get(replayPath, "Authorization: "+credential); response.Code != want {
			t.Fatalf("%s replay=%d", credential, response.Code)
		}
	}
	if response := api.Get("/game/from-lobby/"+uuid.NewString()+"/replay", "Authorization: Bearer alice"); response.Code != http.StatusNotFound {
		t.Fatalf("other lobby replay=%d", response.Code)
	}

	order := fmt.Sprintf(`{"command_type":"send_soldier_group","parameters":{"origin_castle_id":%d,"destination_castle_id":%d,"soldier_amount":5}}`, origin, target)
	if response := commands("Bearer alice", order); response.Code != http.StatusOK {
		t.Fatalf("roster command=%d %s", response.Code, response.Body.String())
	}
	advanced := commands("Bearer bob", "")
	if advanced.Code != http.StatusOK {
		t.Fatalf("roster completion=%d %s", advanced.Code, advanced.Body.String())
	}
	var next models.GenerationState
	if err := json.Unmarshal(advanced.Body.Bytes(), &next); err != nil {
		t.Fatal(err)
	}
	if next.GenerationID == initial.GenerationID || len(next.Generation.SoldierGroups) != 1 {
		t.Fatalf("turn did not advance with alice's order: %+v", next)
	}
	replay := api.Get(replayPath, "Authorization: Bearer bob")
	if replay.Code != http.StatusOK || !strings.Contains(replay.Body.String(), "Alice") || !strings.Contains(replay.Body.String(), "Bob") {
		t.Fatalf("replay=%d %s", replay.Code, replay.Body.String())
	}
	saved, err := dal.GetLobbyGameReplay(database, lobbyID)
	if err != nil {
		t.Fatal(err)
	}
	var frames struct {
		Frames []json.RawMessage `json:"frames"`
	}
	if err := json.Unmarshal(saved, &frames); err != nil || len(frames.Frames) != 2 {
		t.Fatalf("frames=%d err=%v", len(frames.Frames), err)
	}
}
