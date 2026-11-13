package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"slices"
	"strings"
	"testing"
	"time"

	"islandBattles/backend/dal"
	"islandBattles/backend/internal/testutils"
	"islandBattles/backend/models"
	"islandBattles/backend/retention"
	"islandBattles/lobby/adminmcp"
	lobbypostgres "islandBattles/lobby/postgres"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"gorm.io/datatypes"
	gormpostgres "gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// The seeded bearer hash, which no answer may contain.
var bearerHash = bytes.Repeat([]byte{0xAB, 0xCD}, 16)

// fixture holds the seeded rows' IDs.
type fixture struct {
	lobbyID          string
	alice, bob       string
	runningGameID    uint
	finishedGameID   uint
	firstGeneration  uint
	secondGeneration uint
}

// seed creates a started Island Battles lobby with Alice (connected) and
// Bob (not), the game started from it with two generations, and a finished
// game without a lobby. In the second generation, Alice has completed her
// turn and Bob hasn't, and Blue has a soldier group of 5 on its way to Red.
func seed(t *testing.T, database *gorm.DB) fixture {
	t.Helper()
	now := time.Now().UTC()
	ids := fixture{lobbyID: uuid.NewString(), alice: uuid.NewString(), bob: uuid.NewString()}
	blue, red := uuid.NewString(), uuid.NewString()
	alice := ids.alice
	records := []any{
		&lobbypostgres.PlayerRecord{ID: ids.alice, DisplayName: "Alice", CreatedAt: now.Add(-time.Hour)},
		&lobbypostgres.PlayerRecord{ID: ids.bob, DisplayName: "Bob", CreatedAt: now.Add(-time.Hour)},
		&lobbypostgres.SessionRecord{ID: uuid.NewString(), PlayerID: ids.alice, BearerHash: bearerHash, CreatedAt: now.Add(-time.Hour), ExpiresAt: now.Add(time.Hour)},
		&lobbypostgres.RoomRecord{
			ID: ids.lobbyID, GameKey: "island-battles", Name: "Battle", JoinCode: "ABCD2345", Capacity: 2, Phase: "started", Version: 4,
			HostPlayerID: &alice, CreatedAt: now.Add(-time.Hour), RoundStartedAt: now.Add(-time.Hour), NextJoinSequence: 3, GameSettings: []byte(`{}`), LastConnectedAt: now,
		},
		&lobbypostgres.TeamRecord{ID: blue, LobbyID: ids.lobbyID, Name: "Blue", NormalizedName: "blue", TeamOrder: 1},
		&lobbypostgres.TeamRecord{ID: red, LobbyID: ids.lobbyID, Name: "Red", NormalizedName: "red", TeamOrder: 2},
		&lobbypostgres.MembershipRecord{ID: uuid.NewString(), LobbyID: ids.lobbyID, PlayerID: ids.alice, DisplayName: "Alice", Generation: 1, Active: true, TeamID: &blue, Ready: true, JoinedAt: now.Add(-time.Hour), JoinSequence: 1},
		&lobbypostgres.MembershipRecord{ID: uuid.NewString(), LobbyID: ids.lobbyID, PlayerID: ids.bob, DisplayName: "Bob", Generation: 1, Active: true, TeamID: &red, Ready: true, JoinedAt: now.Add(-time.Hour), JoinSequence: 2},
		&lobbypostgres.LeaseRecord{LobbyID: ids.lobbyID, PlayerID: ids.alice, Generation: 1, ClientInstanceID: uuid.NewString(), ExpiresAt: now.Add(time.Minute)},
		&lobbypostgres.LeaseRecord{LobbyID: ids.lobbyID, PlayerID: ids.bob, Generation: 1, ClientInstanceID: uuid.NewString(), ExpiresAt: now.Add(-time.Minute)},
	}
	for _, record := range records {
		create(t, database, record)
	}

	var template models.MapTemplateView
	if err := json.Unmarshal([]byte(testutils.MapExampleInput), &template); err != nil {
		t.Fatal(err)
	}
	templateID, err := dal.CreateMapTemplate(database, template)
	if err != nil {
		t.Fatal(err)
	}
	game := models.Game{
		LobbyID: &ids.lobbyID,
		Teams:   []models.GameTeam{{Color: 1, Name: "Blue", Players: []string{"Alice"}}, {Color: 2, Name: "Red", Players: []string{"Bob"}}},
	}
	create(t, database, &game)
	ids.runningGameID = game.ID
	create(t, database, &[]models.FrozenGamePlayer{{GameID: game.ID, PlayerID: ids.alice, TeamColor: 1, DisplayName: "Alice"}, {GameID: game.ID, PlayerID: ids.bob, TeamColor: 2, DisplayName: "Bob"}})
	if ids.firstGeneration, err = dal.CreateGenerationFromTemplate(database, templateID, game.ID); err != nil {
		t.Fatal(err)
	}
	if ids.secondGeneration, err = dal.CreateGenerationFromTemplate(database, templateID, game.ID); err != nil {
		t.Fatal(err)
	}
	second, err := dal.GetGeneration(database, ids.secondGeneration)
	if err != nil {
		t.Fatal(err)
	}
	clashes := []models.Clash{{ID: 1, Time: 0.5, Participants: []models.ClashParticipant{{GroupID: 9, TeamColor: 2, ClashRadius: 0.25, SoldiersBefore: 3}}}}
	if err := database.Model(second).Select("Clashes").Updates(&models.Generation{Clashes: clashes}).Error; err != nil {
		t.Fatal(err)
	}
	create(t, database, &models.Command{GenerationID: ids.secondGeneration, CommandFields: models.CommandFields{
		CommandType: models.SendSoldierGroup, Parameters: datatypes.JSON(`{"origin_castle_id":1,"destination_castle_id":2,"soldier_amount":5}`),
	}})
	create(t, database, &models.GenerationInput{GenerationID: ids.secondGeneration, PlayerID: ids.alice, Completed: true,
		Commands: []byte(`[{"command_type":"send_soldier_group","parameters":{"origin_castle_id":1,"destination_castle_id":2,"soldier_amount":4}}]`)})
	castles := second.Map.Castles
	slices.SortFunc(castles, func(left, right models.Castle) int { return int(left.TeamColor) - int(right.TeamColor) })
	testutils.CreateSoldierGroupForTest(t, database, &models.SoldierGroup{
		SoldierGroupFields: models.SoldierGroupFields{OriginCastleID: castles[0].ID, DestinationCastleID: castles[1].ID, SoldierAmount: 5},
		GenerationID:       ids.secondGeneration, TeamColor: 1,
	})

	winner := uint(2)
	finished := models.Game{
		Teams:   []models.GameTeam{{Color: 1, Name: "Blue", Players: []string{"Carol"}}, {Color: 2, Name: "Red", Players: []string{"Dave"}}},
		Outcome: &models.GameOutcome{Reason: models.GameOutcomeLastTeamStanding, WinnerTeamColor: &winner},
	}
	create(t, database, &finished)
	ids.finishedGameID = finished.ID
	return ids
}

func create(t *testing.T, database *gorm.DB, record any) {
	t.Helper()
	if err := database.Create(record).Error; err != nil {
		t.Fatal(err)
	}
}

// openAs connects to the test database the way the server does, as the user
// in dsn.
func openAs(t *testing.T, dsn string) *gorm.DB {
	t.Helper()
	database, err := adminmcp.OpenDatabase(context.Background(), dsn, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	sqlDatabase, err := database.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDatabase.Close() })
	return database
}

func dsnOf(t *testing.T, database *gorm.DB) string {
	t.Helper()
	dialector, ok := database.Dialector.(*gormpostgres.Dialector)
	if !ok {
		t.Fatalf("unexpected dialector %T", database.Dialector)
	}
	return dialector.DSN
}

// client serves newServer over an in-memory transport and connects to it.
type client struct {
	session *mcp.ClientSession
	// answers collects every answer's text, to check that none holds a
	// secret.
	answers []string
}

func connect(t *testing.T, database *gorm.DB) *client {
	t.Helper()
	return connectWithRetention(t, database, retention.DefaultPeriod)
}

// connectWithRetention serves newServer with a game retention period of its
// own.
func connectWithRetention(t *testing.T, database *gorm.DB, gameRetentionPeriod time.Duration) *client {
	t.Helper()
	ctx := context.Background()
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	serverSession, err := newServer(database, slog.New(slog.NewTextHandler(io.Discard, nil)), gameRetentionPeriod).Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = serverSession.Close() })
	session, err := mcp.NewClient(&mcp.Implementation{Name: "admin-mcp-test", Version: "test"}, nil).Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = session.Close() })
	return &client{session: session}
}

func (client *client) callText(t *testing.T, name string, arguments map[string]any) (string, bool) {
	t.Helper()
	if arguments == nil {
		arguments = map[string]any{}
	}
	result, err := client.session.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: arguments})
	if err != nil {
		t.Fatalf("call %s: %v", name, err)
	}
	if len(result.Content) != 1 {
		t.Fatalf("%s answered with %d contents", name, len(result.Content))
	}
	text, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("%s answered with %T", name, result.Content[0])
	}
	client.answers = append(client.answers, text.Text)
	return text.Text, result.IsError
}

// call calls a tool that must succeed and decodes its answer into T.
func call[T any](t *testing.T, client *client, name string, arguments map[string]any) T {
	t.Helper()
	text, isError := client.callText(t, name, arguments)
	if isError {
		t.Fatalf("%s(%v) failed: %s", name, arguments, text)
	}
	var answer T
	decoder := json.NewDecoder(strings.NewReader(text))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&answer); err != nil {
		t.Fatalf("decode %s's answer %s: %v", name, text, err)
	}
	return answer
}

// callFailing calls a tool that must fail and returns its error text.
func callFailing(t *testing.T, client *client, name string, arguments map[string]any) string {
	t.Helper()
	text, isError := client.callText(t, name, arguments)
	if !isError {
		t.Fatalf("%s(%v) succeeded with %s", name, arguments, text)
	}
	return text
}

// checkNoBearerHash fails when an answer holds the seeded bearer hash, in
// any encoding, or, with nameToo, even the column's name.
func checkNoBearerHash(t *testing.T, client *client, nameToo bool) {
	t.Helper()
	secrets := []string{hex.EncodeToString(bearerHash), base64.StdEncoding.EncodeToString(bearerHash)}
	if nameToo {
		secrets = append(secrets, "bearer_hash")
	}
	for _, answer := range client.answers {
		for _, secret := range secrets {
			if strings.Contains(strings.ToLower(answer), strings.ToLower(secret)) {
				t.Errorf("an answer contains %s: %s", secret, answer)
			}
		}
	}
}

func TestServerAnswersAboutLobbiesAndGames(t *testing.T) {
	owner := testutils.InitTestDB(t)
	ids := seed(t, owner)
	client := connect(t, openAs(t, dsnOf(t, owner)))

	tools, err := client.session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, tool := range tools.Tools {
		names = append(names, tool.Name)
		if tool.Description == "" || tool.Annotations == nil || !tool.Annotations.ReadOnlyHint {
			t.Errorf("%s has no description or isn't marked read-only", tool.Name)
		}
	}
	slices.Sort(names)
	want := []string{"describe_schema", "find_players", "get_game", "get_generation", "get_lobby", "list_games", "list_lobbies", "overview", "query"}
	if !slices.Equal(names, want) {
		t.Fatalf("tools = %v, want %v", names, want)
	}

	t.Run("overview", func(t *testing.T) {
		var overview struct {
			adminmcp.Overview
			Games GamesOverview `json:"games"`
		}
		text, _ := client.callText(t, "overview", nil)
		if err := json.Unmarshal([]byte(text), &overview); err != nil {
			t.Fatal(err)
		}
		if overview.ActiveLobbyCount != 1 || overview.ConnectedMemberCount != 1 {
			t.Errorf("lobbies = %+v, want one active with Alice connected", overview.Overview)
		}
		if games := overview.Games; games.RunningCount != 1 || games.FinishedCount != 1 || games.GenerationsLastHourCount != 2 ||
			!slices.Equal(games.GenerationsLastHourGameIDs, []uint{ids.runningGameID}) {
			t.Errorf("games = %+v", games)
		}
	})

	t.Run("lobbies link their games", func(t *testing.T) {
		lobby := call[adminmcp.LobbyDetail](t, client, "get_lobby", map[string]any{"lobby_id": ids.lobbyID})
		if lobby.MemberCount != 2 || lobby.ConnectedMemberCount != 1 || lobby.GameID != float64(ids.runningGameID) {
			t.Errorf("lobby = %+v, want 2 members, 1 connected, and game %d", lobby.LobbySummary, ids.runningGameID)
		}
		lobbies := call[adminmcp.LobbyList](t, client, "list_lobbies", nil)
		if len(lobbies.Lobbies) != 1 || lobbies.Lobbies[0].GameID != float64(ids.runningGameID) {
			t.Errorf("lobbies = %+v", lobbies)
		}
	})

	t.Run("list_games", func(t *testing.T) {
		all := call[GameList](t, client, "list_games", nil)
		if len(all.Games) != 2 || all.Games[0].ID != ids.finishedGameID || all.Games[1].ID != ids.runningGameID || all.Truncated {
			t.Fatalf("games = %+v, want both, newest first", all)
		}
		finished, running := all.Games[0], all.Games[1]
		if finished.Status != "finished" || finished.Outcome == nil || finished.Outcome.Reason != "last_team_standing" ||
			finished.Outcome.WinnerTeamName == nil || *finished.Outcome.WinnerTeamName != "Red" || finished.GenerationCount != 0 ||
			finished.LastGenerationAt != nil || !finished.RetentionDeletesAt.Equal(finished.CreatedAt.Add(7*24*time.Hour)) {
			t.Errorf("finished game = %+v", finished)
		}
		if running.Status != "running" || running.Outcome != nil || running.GenerationCount != 2 || running.LastGenerationAt == nil ||
			!running.RetentionDeletesAt.Equal(running.LastGenerationAt.Add(7*24*time.Hour)) || running.LobbyID == nil || *running.LobbyID != ids.lobbyID {
			t.Errorf("running game = %+v", running)
		}
		wantPlayers := []RosterPlayer{{PlayerID: ids.alice, DisplayName: pointer("Alice"), TeamColor: 1}, {PlayerID: ids.bob, DisplayName: pointer("Bob"), TeamColor: 2}}
		if !equalJSON(running.Players, wantPlayers) || len(running.Teams) != 2 || running.Teams[0].Name != "Blue" || running.Teams[0].Players[0] != "Alice" {
			t.Errorf("roster = %+v, teams = %+v", running.Players, running.Teams)
		}
		if finishedOnly := call[GameList](t, client, "list_games", map[string]any{"status": "finished"}); len(finishedOnly.Games) != 1 || finishedOnly.Games[0].ID != ids.finishedGameID {
			t.Errorf("finished games = %+v", finishedOnly)
		}
		if limited := call[GameList](t, client, "list_games", map[string]any{"limit": 1}); len(limited.Games) != 1 || !limited.Truncated {
			t.Errorf("limited games = %+v", limited)
		}
		callFailing(t, client, "list_games", map[string]any{"status": "abandoned"})
	})

	t.Run("get_game", func(t *testing.T) {
		game := call[GameDetail](t, client, "get_game", map[string]any{"lobby_id": ids.lobbyID})
		if game.ID != ids.runningGameID || game.CurrentGeneration == nil {
			t.Fatalf("game = %+v", game)
		}
		current := game.CurrentGeneration
		if current.ID != ids.secondGeneration || current.Turn != 2 {
			t.Errorf("current generation = %+v, want turn 2", current)
		}
		if len(current.TurnStatus) != 2 || !current.TurnStatus[0].Completed || current.TurnStatus[1].Completed {
			t.Errorf("turn status = %+v, want Alice done and Bob not", current.TurnStatus)
		}
		wantTeams := []TeamState{
			{Color: 1, Name: pointer("Blue"), Castles: 1, SoldiersInCastles: 15, SoldierGroups: 1, SoldiersInGroups: 5},
			{Color: 2, Name: pointer("Red"), Castles: 1, SoldiersInCastles: 15},
		}
		if !equalJSON(current.Teams, wantTeams) {
			t.Errorf("teams = %+v, want %+v", current.Teams, wantTeams)
		}
		if byID := call[GameDetail](t, client, "get_game", map[string]any{"game_id": ids.finishedGameID}); byID.CurrentGeneration != nil || byID.Status != "finished" {
			t.Errorf("finished game = %+v, want no generation", byID)
		}
		for _, arguments := range []map[string]any{{}, {"game_id": 999999}, {"lobby_id": "x"}, {"game_id": ids.runningGameID, "lobby_id": ids.lobbyID}} {
			callFailing(t, client, "get_game", arguments)
		}
	})

	t.Run("get_generation", func(t *testing.T) {
		first := call[GenerationDetail](t, client, "get_generation", map[string]any{"game_id": ids.runningGameID, "turn": 1})
		if first.ID != ids.firstGeneration || first.Turn != 1 || first.Current || first.Board != nil || len(first.AppliedCommands) != 0 {
			t.Errorf("first generation = %+v", first)
		}
		second := call[GenerationDetail](t, client, "get_generation", map[string]any{"game_id": ids.runningGameID, "generation_id": ids.secondGeneration, "include_board": true})
		if second.Turn != 2 || !second.Current || len(second.Clashes) != 1 || len(second.AppliedCommands) != 1 || second.AppliedCommands[0].Type != "send_soldier_group" {
			t.Errorf("second generation = %+v", second)
		}
		if len(second.PlayerCommands) != 2 || !second.PlayerCommands[0].Completed || len(second.PlayerCommands[0].Commands.([]any)) != 1 ||
			second.PlayerCommands[1].Completed || len(second.PlayerCommands[1].Commands.([]any)) != 0 || second.PlayerCommands[1].UpdatedAt != nil {
			t.Errorf("player commands = %+v", second.PlayerCommands)
		}
		if second.Board == nil || second.Board.ID != ids.secondGeneration || len(second.Board.Castles) != 2 || len(second.Board.SoldierGroups) != 1 ||
			second.Board.SoldierGroups[0].OriginCastleID == second.Board.SoldierGroups[0].DestinationCastleID {
			t.Errorf("board = %+v", second.Board)
		} else {
			// The board carries every upgrade level, as players receive it.
			if levels := second.Board.Castles[0].Upgrades; len(levels) == 0 || !equalJSON(levels, models.Upgrades{}.Clone()) {
				t.Errorf("castle upgrades = %v", levels)
			}
			if levels := second.Board.SoldierGroups[0].Upgrades; len(levels) == 0 || !equalJSON(levels, models.Upgrades{}.Clone()) {
				t.Errorf("soldier group upgrades = %v", levels)
			}
		}
		for _, arguments := range []map[string]any{
			{"game_id": ids.runningGameID}, {"game_id": ids.runningGameID, "turn": 3}, {"game_id": ids.finishedGameID, "generation_id": ids.firstGeneration},
			{"game_id": ids.runningGameID, "turn": 1, "generation_id": ids.firstGeneration},
		} {
			callFailing(t, client, "get_generation", arguments)
		}
	})

	t.Run("describe_schema", func(t *testing.T) {
		tables := call[Schema](t, client, "describe_schema", nil)
		var tableNames []string
		for _, table := range tables.Tables {
			tableNames = append(tableNames, table.Name)
		}
		for _, table := range []string{"lobby_rooms", "lobby_memberships", "games", "generations", "castles"} {
			if !slices.Contains(tableNames, table) {
				t.Errorf("tables = %v, missing %s", tableNames, table)
			}
		}
		rooms := call[Schema](t, client, "describe_schema", map[string]any{"table": "lobby_rooms"})
		if len(rooms.Columns) == 0 || rooms.Columns[0].Name != "id" || rooms.Columns[0].Type != "uuid" || rooms.Columns[0].Nullable {
			t.Errorf("lobby_rooms' columns = %+v", rooms.Columns)
		}
		callFailing(t, client, "describe_schema", map[string]any{"table": "no_such_table"})
	})

	t.Run("no answer holds a bearer hash", func(t *testing.T) {
		// The owner may read the column, and describe_schema names it.
		call[QueryResult](t, client, "query", map[string]any{"sql": "SELECT * FROM lobby_sessions"})
		checkNoBearerHash(t, client, false)
	})
}

func pointer[T any](value T) *T { return &value }

// equalJSON compares two values by their JSON.
func equalJSON(left, right any) bool {
	leftJSON, leftErr := json.Marshal(left)
	rightJSON, rightErr := json.Marshal(right)
	return leftErr == nil && rightErr == nil && bytes.Equal(leftJSON, rightJSON)
}
