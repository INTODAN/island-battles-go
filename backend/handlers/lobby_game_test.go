package handlers_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/humatest"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"islandBattles/backend/handlers"
	"islandBattles/backend/internal/testutils"
	"islandBattles/backend/models"
	"islandBattles/backend/services"
	"islandBattles/lobby/domain"
	"islandBattles/lobby/playertoken"
)

// bearerPlayers maps whole Authorization headers to players.
type bearerPlayers map[string]domain.Principal

func (players bearerPlayers) AuthenticatePlayer(_ context.Context, authorization string) (domain.Principal, error) {
	principal, exists := players[authorization]
	if !exists {
		return domain.Principal{}, playertoken.ErrInvalid
	}
	return principal, nil
}

// requireNoLobbyTables checks that Island Battles' database has none of the
// lobby's tables, which are in the lobby's own database (#229), so the game
// works as if its lobby were deleted. Authentication is faked here, so a game
// request that still read any lobby table would fail.
func requireNoLobbyTables(t *testing.T, database *gorm.DB) {
	t.Helper()
	tables, err := database.Migrator().GetTables()
	if err != nil {
		t.Fatal(err)
	}
	for _, table := range tables {
		if strings.HasPrefix(table, "lobby_") {
			t.Fatalf("Island Battles' database has the lobby's table %s", table)
		}
	}
}

// watchingPlayers stands in for the lobby's watcher rows.
type watchingPlayers struct{ watching, kicked map[string]bool }

func (watchers watchingPlayers) AuthorizeWatch(_ context.Context, principal domain.Principal, _ domain.Scope) error {
	switch {
	case watchers.watching[principal.PlayerID]:
		return nil
	case watchers.kicked[principal.PlayerID]:
		return domain.ErrWatcherKicked
	}
	return domain.ErrNotFound
}

// A watcher gets the replay page built for watchers; a kicked one is refused,
// and anyone else finds no game.
func TestWatcherReplayIsServedToWatchersOnly(t *testing.T) {
	database := testutils.InitTestDB(t)
	alice, bob, lobbyID, red, blue := uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString()
	if _, err := services.RunLobbyGame(context.Background(), database, &testutils.RecordedPublisher{}, nil, domain.Snapshot{
		Lobby:       domain.Lobby{ID: lobbyID, GameKey: "island-battles", Phase: domain.PhaseStarted},
		Teams:       []domain.Team{{ID: red, Name: "Red", Order: 1}, {ID: blue, Name: "Blue", Order: 2}},
		StartRoster: domain.StartRoster{{PlayerID: alice, TeamID: red, TeamOrder: 1}, {PlayerID: bob, TeamID: blue, TeamOrder: 2}},
		Members:     []domain.Member{{PlayerID: alice, DisplayName: "Alice"}, {PlayerID: bob, DisplayName: "Bob"}},
	}); err != nil {
		t.Fatal(err)
	}
	watcher, kicked := uuid.NewString(), uuid.NewString()
	handler := handlers.LobbyGame{Database: database, Publisher: &testutils.RecordedPublisher{},
		Watchers: watchingPlayers{watching: map[string]bool{watcher: true}, kicked: map[string]bool{kicked: true}},
		Players:  bearerPlayers{"Bearer alice": {PlayerID: alice}, "Bearer watcher": {PlayerID: watcher}, "Bearer kicked": {PlayerID: kicked}, "Bearer outsider": {PlayerID: uuid.NewString()}}}
	_, api := humatest.New(t)
	huma.Register(api, huma.Operation{OperationID: "watchReplay", Method: http.MethodGet, Path: "/game/from-lobby/{lobby_id}/watch/replay"}, handler.WatchReplay)
	huma.Register(api, huma.Operation{OperationID: "replay", Method: http.MethodGet, Path: "/game/from-lobby/{lobby_id}/replay"}, handler.Replay)
	path := "/game/from-lobby/" + lobbyID + "/watch/replay"

	for credential, want := range map[string]int{"Bearer watcher": http.StatusOK, "Bearer alice": http.StatusOK, "Bearer kicked": http.StatusForbidden, "Bearer outsider": http.StatusNotFound, "Bearer stranger": http.StatusUnauthorized} {
		if response := api.Get(path, "Authorization: "+credential); response.Code != want {
			t.Fatalf("%s watch replay=%d %s", credential, response.Code, response.Body.String())
		}
	}
	page := api.Get(path, "Authorization: Bearer watcher")
	if !strings.Contains(page.Body.String(), "const watcher = true;") || !strings.HasPrefix(page.Header().Get("Content-Type"), "text/html") || page.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("watcher's page: %q %q", page.Header().Get("Content-Type"), page.Header().Get("Cache-Control"))
	}
	// A watcher never reads the players' replay.
	if response := api.Get("/game/from-lobby/"+lobbyID+"/replay", "Authorization: Bearer watcher"); response.Code != http.StatusNotFound {
		t.Fatalf("watcher's read of the players' replay=%d", response.Code)
	}
	if response := api.Get("/game/from-lobby/"+uuid.NewString()+"/watch/replay", "Authorization: Bearer watcher"); response.Code != http.StatusNotFound {
		t.Fatalf("unknown lobby=%d", response.Code)
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
	if _, err := services.RunLobbyGame(context.Background(), database, &testutils.RecordedPublisher{}, nil, snapshot); err != nil {
		t.Fatal(err)
	}
	// Carol plays a different game, so she is on a roster but not on this one.
	carol, otherLobby, green, yellow := uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString()
	if _, err := services.RunLobbyGame(context.Background(), database, &testutils.RecordedPublisher{}, nil, domain.Snapshot{
		Lobby:       domain.Lobby{ID: otherLobby, GameKey: "island-battles", Phase: domain.PhaseStarted},
		Teams:       []domain.Team{{ID: green, Name: "Green", Order: 1}, {ID: yellow, Name: "Yellow", Order: 2}},
		StartRoster: domain.StartRoster{{PlayerID: carol, TeamID: green, TeamOrder: 1}, {PlayerID: uuid.NewString(), TeamID: yellow, TeamOrder: 2}},
	}); err != nil {
		t.Fatal(err)
	}
	requireNoLobbyTables(t, database)
	handler := handlers.LobbyGame{Database: database, Publisher: &testutils.RecordedPublisher{}, Players: bearerPlayers{
		"Bearer alice": {PlayerID: alice}, "Bearer bob": {PlayerID: bob}, "Bearer outsider": {PlayerID: uuid.NewString()}, "Bearer carol": {PlayerID: carol},
	}}
	_, api := humatest.New(t)
	huma.Register(api, huma.Operation{OperationID: "replay", Method: http.MethodGet, Path: "/game/from-lobby/{lobby_id}/replay"}, handler.Replay)
	huma.Register(api, huma.Operation{OperationID: "current", Method: http.MethodGet, Path: "/game/from-lobby/{lobby_id}/generation"}, handler.CurrentGeneration)
	// The service validates the body itself, as routes.SetupGameRoutes registers it.
	huma.Register(api, huma.Operation{OperationID: "commands", Method: http.MethodPost, Path: "/generation/commands", SkipValidateBody: true}, handler.RegisterGenerationCommands)
	huma.Register(api, huma.Operation{OperationID: "logs", Method: http.MethodGet, Path: "/game/from-lobby/{lobby_id}/logs"}, handler.GenerationLogs)
	huma.Register(api, huma.Operation{OperationID: "demo", Method: http.MethodGet, Path: "/game/from-lobby/{lobby_id}/demo"}, handler.Demo)
	statePath, replayPath, demoPath := "/game/from-lobby/"+lobbyID+"/generation", "/game/from-lobby/"+lobbyID+"/replay", "/game/from-lobby/"+lobbyID+"/demo"

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
	// Callers off this game's roster are rejected, even when another game's
	// roster has them, and unknown credentials stay unauthenticated.
	for credential, want := range map[string]int{"Bearer outsider": http.StatusNotFound, "Bearer carol": http.StatusNotFound, "Bearer stranger": http.StatusUnauthorized} {
		if response := api.Get(statePath, "Authorization: "+credential); response.Code != want {
			t.Fatalf("%s read=%d", credential, response.Code)
		}
		if response := commands(credential, ""); response.Code != want {
			t.Fatalf("%s commands=%d", credential, response.Code)
		}
		if response := api.Get(replayPath, "Authorization: "+credential); response.Code != want {
			t.Fatalf("%s replay=%d", credential, response.Code)
		}
		if response := api.Get(fmt.Sprintf("/game/from-lobby/%s/logs?generation_id=%d", lobbyID, initial.GenerationID), "Authorization: "+credential); response.Code != want {
			t.Fatalf("%s logs=%d", credential, response.Code)
		}
		if response := api.Get(demoPath, "Authorization: "+credential); response.Code != want || response.Header().Get("Content-Disposition") != "" {
			t.Fatalf("%s demo=%d %s", credential, response.Code, response.Body.String())
		}
	}
	if response := api.Get("/game/from-lobby/"+uuid.NewString()+"/demo", "Authorization: Bearer alice"); response.Code != http.StatusNotFound {
		t.Fatalf("unknown lobby demo=%d", response.Code)
	}
	if response := api.Get("/game/from-lobby/"+uuid.NewString()+"/replay", "Authorization: Bearer alice"); response.Code != http.StatusNotFound {
		t.Fatalf("unknown lobby replay=%d", response.Code)
	}
	if response := api.Get("/game/from-lobby/"+otherLobby+"/generation", "Authorization: Bearer carol"); response.Code != http.StatusOK {
		t.Fatalf("carol's own game read=%d %s", response.Code, response.Body.String())
	}

	// Game log lines travel in the same batches, and their limits answer a
	// problem type of their own.
	logBatch := func(message string) *httptest.ResponseRecorder {
		request := fmt.Sprintf(`{"game_id":%d,"generation_id":%d,"request_id":%q,"complete":false,"commands":[],"logs":[{"level":"info","logger":"island_battles.player","message":%q}]}`,
			initial.GameID, initial.GenerationID, uuid.NewString(), message)
		return api.Post("/generation/commands", "Authorization: Bearer alice", strings.NewReader(request))
	}
	if response := logBatch("attacking with 5"); response.Code != http.StatusOK {
		t.Fatalf("log batch=%d %s", response.Code, response.Body.String())
	}
	if response := logBatch(strings.Repeat("x", 501)); response.Code != http.StatusUnprocessableEntity || !strings.Contains(response.Body.String(), services.GameLogLimitProblemType) {
		t.Fatalf("long line=%d %s", response.Code, response.Body.String())
	}
	logsPath := fmt.Sprintf("/game/from-lobby/%s/logs?generation_id=%d", lobbyID, initial.GenerationID)
	readLogs := func(credential string) models.GenerationLogs {
		t.Helper()
		response := api.Get(logsPath, "Authorization: "+credential)
		if response.Code != http.StatusOK {
			t.Fatalf("logs=%d %s", response.Code, response.Body.String())
		}
		var logs models.GenerationLogs
		if err := json.Unmarshal(response.Body.Bytes(), &logs); err != nil {
			t.Fatal(err)
		}
		return logs
	}
	if logs := readLogs("Bearer alice"); len(logs.Players) != 1 || logs.Players[0].DisplayName != "Alice" || len(logs.Players[0].Lines) != 1 || logs.Players[0].Lines[0].Message != "attacking with 5" {
		t.Fatalf("Alice's logs = %+v", logs)
	}
	if logs := readLogs("Bearer bob"); len(logs.Players) != 1 || logs.Players[0].DisplayName != "Bob" || len(logs.Players[0].Lines) != 0 || len(logs.HiddenTeamColors) != 1 {
		t.Fatalf("Bob's logs = %+v", logs)
	}
	if response := api.Get("/game/from-lobby/"+lobbyID+"/logs", "Authorization: Bearer alice"); response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("logs without a generation=%d", response.Code)
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
	// The page embeds one frame per stored generation, the last one being the
	// generation the turn just produced.
	_, embedded, found := strings.Cut(replay.Body.String(), "let replay = ")
	embedded, _, _ = strings.Cut(embedded, ";\n")
	var page services.GameReplay
	if err := json.Unmarshal([]byte(embedded), &page); !found || err != nil {
		t.Fatalf("embedded replay %q: %v", embedded, err)
	}
	if len(page.Frames) != 2 || page.Frames[1].ID != next.GenerationID || page.GameID != initial.GameID {
		t.Fatalf("replay page = %+v", page)
	}

	// A demo is a download of the same frames, with the logs its saver may
	// see: during the game, Bob sees no line of Alice's.
	saveDemo := func(credential string) models.Demo {
		t.Helper()
		response := api.Get(demoPath, "Authorization: "+credential)
		if response.Code != http.StatusOK {
			t.Fatalf("%s demo=%d %s", credential, response.Code, response.Body.String())
		}
		wantName := fmt.Sprintf(`attachment; filename="island-battles-game-%d-generation-2.json"`, initial.GameID)
		if response.Header().Get("Content-Disposition") != wantName || response.Header().Get("Content-Type") != "application/json" || response.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("demo headers = %v", response.Header())
		}
		var demo models.Demo
		if err := json.Unmarshal(response.Body.Bytes(), &demo); err != nil {
			t.Fatal(err)
		}
		if demo.Format != models.DemoFormat || demo.SchemaVersion != models.DemoSchemaVersion || demo.GameID != initial.GameID || !reflect.DeepEqual(demo.Frames, page.Frames) || len(demo.Logs) != 2 {
			t.Fatalf("demo = %+v", demo)
		}
		return demo
	}
	if demo := saveDemo("Bearer bob"); demo.Outcome != nil || len(demo.Logs[0].Players) != 1 || demo.Logs[0].Players[0].DisplayName != "Bob" || !reflect.DeepEqual(demo.Logs[0].HiddenTeamColors, []uint{initial.TeamColor}) {
		t.Fatalf("Bob's demo logs = %+v", demo.Logs)
	}
	if demo := saveDemo("Bearer alice"); len(demo.Logs[0].Players) != 1 || len(demo.Logs[0].Players[0].Lines) != 1 || demo.Logs[0].Players[0].Lines[0].Message != "attacking with 5" {
		t.Fatalf("Alice's demo logs = %+v", demo.Logs)
	}
}
