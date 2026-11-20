package handlers_test

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/humatest"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/INTODAN/mpl/domain"
	"islandBattles/backend/dal"
	"islandBattles/backend/handlers"
	"islandBattles/backend/internal/testutils"
	"islandBattles/backend/models"
	"islandBattles/backend/retention"
	"islandBattles/backend/services"
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
	binding, err := services.RunLobbyGame(context.Background(), database, &testutils.RecordedPublisher{}, nil, domain.Snapshot{
		Lobby: domain.Lobby{ID: lobbyID, GameKey: "island-battles", Phase: domain.PhaseStarted, GameSettings: testutils.CircleMapSettings(2, "")},
		Teams: []domain.Team{{ID: teamID, Order: 1}, {ID: uuid.NewString(), Order: 2}}, StartRoster: domain.StartRoster{{PlayerID: playerID, TeamID: teamID}},
	})
	if err != nil {
		t.Fatal(err)
	}
	retention := &countingRetention{database: database, gameID: binding.GameID}
	handler := handlers.LobbyGame{Database: database, Players: commandAuthentication{domain.Principal{PlayerID: playerID}},
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

// Accounts and competitors outlive every game: a retention pass that deletes
// an expired game deletes none of them, deleted bot names included.
func TestRetentionKeepsAccountsAndCompetitors(t *testing.T) {
	database := testutils.InitTestDB(t)
	ctx := context.Background()
	accounts := services.Accounts{Database: database}
	dana := domain.Principal{PlayerID: uuid.NewString(), DisplayName: "Dana", AccountID: uuid.NewString()}
	kraken, err := accounts.RegisterBotName(ctx, dana, "Kraken")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := accounts.RegisterBotName(ctx, dana, "Leviathan"); err != nil {
		t.Fatal(err)
	}
	if err := accounts.DeleteBotName(ctx, dana, kraken.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := dal.FindOrCreateHouseCompetitor(database, 2); err != nil {
		t.Fatal(err)
	}
	count := func() (accountRows, competitorRows int64) {
		t.Helper()
		if err := database.Model(&models.IslandAccount{}).Count(&accountRows).Error; err != nil {
			t.Fatal(err)
		}
		if err := database.Unscoped().Model(&models.Competitor{}).Count(&competitorRows).Error; err != nil {
			t.Fatal(err)
		}
		return accountRows, competitorRows
	}
	accountRows, competitorRows := count()
	if accountRows != 1 || competitorRows != 4 {
		t.Fatalf("%d accounts and %d competitors before retention, want 1 and 4", accountRows, competitorRows)
	}

	playerID, teamID := uuid.NewString(), uuid.NewString()
	binding, err := services.RunLobbyGame(ctx, database, &testutils.RecordedPublisher{}, nil, domain.Snapshot{
		Lobby: domain.Lobby{ID: uuid.NewString(), GameKey: "island-battles", Phase: domain.PhaseStarted, GameSettings: testutils.CircleMapSettings(2, `{"turn_timeout_seconds":null}`)},
		Teams: []domain.Team{{ID: teamID, Order: 1}, {ID: uuid.NewString(), Order: 2}}, StartRoster: domain.StartRoster{{PlayerID: playerID, TeamID: teamID}},
	})
	if err != nil {
		t.Fatal(err)
	}
	runner, err := retention.New(retention.DatabaseStore{Database: database}, retention.DefaultPolicy(), time.Now, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	// Long after the game's last generation, a few passes retire and purge
	// it.
	later := time.Now().Add(30 * 24 * time.Hour)
	for range 3 {
		if _, err := runner.Pass(ctx, later); err != nil {
			t.Fatal(err)
		}
	}
	var games int64
	if err := database.Unscoped().Model(&models.Game{}).Where(&models.Game{Model: gorm.Model{ID: binding.GameID}}).Count(&games).Error; err != nil {
		t.Fatal(err)
	}
	if games != 0 {
		t.Fatal("retention kept the expired game")
	}
	if got, gotCompetitors := count(); got != accountRows || gotCompetitors != competitorRows {
		t.Fatalf("%d accounts and %d competitors after retention, want %d and %d", got, gotCompetitors, accountRows, competitorRows)
	}
}
