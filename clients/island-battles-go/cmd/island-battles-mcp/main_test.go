package main

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	islandbattles "github.com/INTODAN/island-battles-go/clients/island-battles-go"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// The fixture, testdata/demo.json, is a finished six-turn game of Blue
// (Alex) against Red (Sam) on castles 0 (Blue), 1 (Red) and 2 (neutral):
//
//	turn 2: Blue's group 0 (6) marches from castle 0 to castle 2, Red's group 1 (8) from castle 1 to castle 0.
//	turn 3: group 0 takes castle 2 from neutral (6 against 5).
//	turn 4: group 1 takes castle 0 (8 against 6 + 1 growth); castle 2 pays 1 soldier for speed 2.
//	turn 5: Blue's group 2 (1) and Red's group 3 (4) clash; group 2 is defeated, sent and gone within the turn.
//	turn 6: group 3 takes castle 2 (3 against 1 + 1), and Red wins.
const fixturePath = "testdata/demo.json"

// testClient calls the server's tools over an in-memory transport.
type testClient struct {
	session *mcp.ClientSession
}

func connect(t *testing.T, games *store) *testClient {
	t.Helper()
	ctx := context.Background()
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	serverSession, err := newServer(games).Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = serverSession.Close() })
	session, err := mcp.NewClient(&mcp.Implementation{Name: "island-battles-mcp-test", Version: "test"}, nil).Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = session.Close() })
	return &testClient{session: session}
}

func quietLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// connectWithFixture serves the tools with the fixture loaded.
func connectWithFixture(t *testing.T) *testClient {
	t.Helper()
	client := connect(t, newStore(newLobbyClient("http://lobby.invalid", "http://game.invalid", "AI agent", http.DefaultClient, quietLogger()), quietLogger()))
	call[Overview](t, client, "load_demo", map[string]any{"path": fixturePath})
	return client
}

// callText calls a tool and returns its text answer and whether it failed.
func (client *testClient) callText(t *testing.T, name string, arguments map[string]any) (string, bool) {
	t.Helper()
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
	return text.Text, result.IsError
}

// call calls a tool that must succeed and decodes its answer into T, which
// must describe every field of it.
func call[T any](t *testing.T, client *testClient, name string, arguments map[string]any) T {
	t.Helper()
	text, failed := client.callText(t, name, arguments)
	if failed {
		t.Fatalf("%s(%v) failed: %s", name, arguments, text)
	}
	var answer T
	decoder := json.NewDecoder(strings.NewReader(text))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&answer); err != nil {
		t.Fatalf("%s answered %s: %v", name, text, err)
	}
	return answer
}

// fails calls a tool that must fail, and returns its message.
func fails(t *testing.T, client *testClient, name string, arguments map[string]any) string {
	t.Helper()
	text, failed := client.callText(t, name, arguments)
	if !failed {
		t.Fatalf("%s(%v) succeeded: %s", name, arguments, text)
	}
	return text
}

func TestTheOverviewListsTeamsCapturesAndTheEnd(t *testing.T) {
	client := connectWithFixture(t)
	overview := call[Overview](t, client, "game_overview", nil)
	if overview.Game != "demo.json" || overview.GameID != 7 || overview.Running || overview.Turns != 6 || overview.Outcome == nil || overview.Outcome.Summary != "Red wins as the last team standing" {
		t.Fatalf("overview = %+v", overview)
	}
	wantCaptures := []string{"turn 3: castle 2, neutral → Blue", "turn 4: castle 0, Blue → Red", "turn 6: castle 2, Blue → Red"}
	if !reflect.DeepEqual(overview.Captures, wantCaptures) {
		t.Fatalf("captures = %q, want %q", overview.Captures, wantCaptures)
	}
	if len(overview.Teams) != 2 || overview.Teams[0].Team != "Blue" || overview.Teams[0].EliminatedOnTurn != 6 || overview.Teams[1].EliminatedOnTurn != 0 ||
		!reflect.DeepEqual(overview.Teams[1].Players, []string{"Sam"}) {
		t.Fatalf("teams = %+v", overview.Teams)
	}
	if want := []TeamTotals{{Team: "Blue"}, {Team: "Red", Castles: 3, CastleSoldiers: 7}}; !reflect.DeepEqual(overview.LastTurn, want) {
		t.Fatalf("last turn = %+v, want %+v", overview.LastTurn, want)
	}
}

func TestExplainCastleTellsHowACastleWasCaptured(t *testing.T) {
	client := connectWithFixture(t)
	explanation := call[CastleExplanation](t, client, "explain_castle", map[string]any{"castle_id": 0, "turn": 4})
	ledger := explanation.Ledger
	if ledger == nil || ledger.OwnerBefore != "Blue" || ledger.OwnerAfter != "Red" || ledger.SoldiersBefore != 6 || ledger.SoldiersAfter != 1 ||
		ledger.Growth != 1 || ledger.GarrisonBeforeArrivals != 7 || !ledger.Reconciled {
		t.Fatalf("ledger = %+v", ledger)
	}
	wantStep := ArrivalStep{GroupID: 1, Team: "Red", FromCastle: 1, SentOnTurn: 1, SoldiersSent: 8, Soldiers: 8, GarrisonBefore: 7, Result: "captured", GarrisonAfter: 1, OwnerAfter: "Red"}
	if !reflect.DeepEqual(ledger.Arrivals, []ArrivalStep{wantStep}) {
		t.Fatalf("arrivals = %+v", ledger.Arrivals)
	}
	if !strings.Contains(explanation.Summary, "Castle 0 went from Blue to Red on turn 4 when group 1 arrived with 8 soldiers against 7 defenders") {
		t.Fatalf("summary = %q", explanation.Summary)
	}
	if len(explanation.Attackers) != 1 || explanation.Attackers[0].SentOnTurn != 1 || explanation.Attackers[0].Arrived != 8 {
		t.Fatalf("attackers = %+v", explanation.Attackers)
	}
	// Red's reasons come from turn 1, when its commands sent group 1, and
	// Blue's from the turn before the capture.
	want := []TeamTurnLogs{
		{Team: "Red", Turn: 1, Why: "the turn whose commands sent group 1", Lines: []string{"Sam: debug island_battles.player: reading the board", "Sam: info island_battles.player: attacking castle 0 with 8"}},
		{Team: "Blue", Turn: 3, Why: "the owner's commands of the turn before", Lines: []string{"Alex: info island_battles.player: upgrading speed at castle 2"}},
		{Team: "Red", Turn: 3, Why: "the new owner's commands of the turn before", Lines: []string{}},
	}
	if !reflect.DeepEqual(explanation.Logs, want) {
		t.Fatalf("logs = %+v, want %+v", explanation.Logs, want)
	}
	if explanation.LastOwnerChange != "" || explanation.NextOwnerChange != "" {
		t.Fatalf("owner changes = %q, %q", explanation.LastOwnerChange, explanation.NextOwnerChange)
	}

	// A turn without a capture points at the captures around it.
	later := call[CastleExplanation](t, client, "explain_castle", map[string]any{"castle_id": 2, "turn": 5})
	if later.LastOwnerChange != "turn 3: neutral → Blue" || later.NextOwnerChange != "turn 6: Blue → Red" || !strings.Contains(later.Summary, "stayed Blue's") {
		t.Fatalf("castle 2 on turn 5 = %+v", later)
	}
	// Group 2 shows only in turn 5's clash, so its soldiers are explained by
	// the note.
	if later.Ledger.UpgradeCost == nil || *later.Ledger.UpgradeCost != 1 || !strings.Contains(later.Ledger.Note, "group 2") {
		t.Fatalf("castle 2 ledger on turn 5 = %+v", later.Ledger)
	}
	if !reflect.DeepEqual(later.MarchingTo, []string{"Red group 3, 3 soldiers from castle 1, 2.12 away"}) {
		t.Fatalf("marching to castle 2 = %q", later.MarchingTo)
	}
}

func TestTheLedgerWorksOutWhatUpgradesCost(t *testing.T) {
	client := connectWithFixture(t)
	explanation := call[CastleExplanation](t, client, "explain_castle", map[string]any{"castle_id": 2, "turn": 4})
	ledger := explanation.Ledger
	if ledger == nil || !reflect.DeepEqual(ledger.UpgradesBought, map[string]int{"speed": 2}) || ledger.UpgradeCost == nil || *ledger.UpgradeCost != 1 || !ledger.Reconciled || ledger.Note != "" {
		t.Fatalf("ledger = %+v", ledger)
	}
}

// Working out an upgrade cost tries a bounded number of costs, so a crafted
// demo with a huge garrison can't hold up loading it.
func TestTheUpgradeCostSearchIsBounded(t *testing.T) {
	demo, err := readDemoFile(fixturePath)
	if err != nil {
		t.Fatal(err)
	}
	game := newGame("demo.json", "demo file", demo)
	before := islandbattles.Castle{ID: 99, TeamColor: 1, Soldiers: 4_000_000_000}
	after := islandbattles.Castle{ID: 99, TeamColor: 2, Soldiers: 7}
	changes := turnChanges{arrived: []arrivedGroup{{id: 500, team: 2, from: 1, castle: 99, soldiers: 5}}}
	started := time.Now()
	ledger := game.ledger(2, before, after, changes)
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("the search took %s", elapsed)
	}
	if ledger.Reconciled || !strings.Contains(ledger.Note, "wasn't worked out") {
		t.Fatalf("ledger = %+v", ledger)
	}
	// Without arrivals the cost needs no search, whatever the garrison.
	alone := game.ledger(2, before, islandbattles.Castle{ID: 99, TeamColor: 1, Soldiers: 3_999_999_991}, turnChanges{})
	if !alone.Reconciled || alone.UpgradeCost == nil || *alone.UpgradeCost != 10 {
		t.Fatalf("ledger without arrivals = %+v", alone)
	}
	// The whole game's search is bounded too: a cost of 2 is found until the
	// game's search is spent.
	small, held := islandbattles.Castle{ID: 99, TeamColor: 1, Soldiers: 10}, islandbattles.Castle{ID: 99, TeamColor: 1, Soldiers: 4}
	if found := game.ledger(2, small, held, changes); !found.Reconciled || found.UpgradeCost == nil || *found.UpgradeCost != 2 {
		t.Fatalf("ledger = %+v", found)
	}
	game.costSearch = 0
	if spent := game.ledger(2, small, held, changes); spent.Reconciled || !strings.Contains(spent.Note, "wasn't worked out") {
		t.Fatalf("ledger past the game's search = %+v", spent)
	}
}

// webFixtures are the web client's demos of real bot games, which the game's
// own simulation writes. heading-arrows.json is edited by hand, so its
// boards don't follow from each other.
const webFixtures = "../../../island-battles-web/test/fixtures"

// A castle grows by its generation_amount level, plus a burst that arrived
// in the turn, less what repays its burst debt, as the game plays it.
func TestGrowthFollowsProductionAndBursts(t *testing.T) {
	castle := func(level int, debt uint) islandbattles.Castle {
		return islandbattles.Castle{TeamColor: 1, BurstDebt: debt, Upgrades: islandbattles.Upgrades{islandbattles.GenerationAmountUpgrade: level}}
	}
	for name, test := range map[string]struct {
		before, after islandbattles.Castle
		want          uint
	}{
		"level 1":                  {castle(1, 0), castle(1, 0), 1},
		"level 3":                  {castle(3, 0), castle(3, 0), 3},
		"bought level 2 this turn": {castle(1, 0), castle(2, 0), 2},
		"a burst of 8 arrives":     {castle(3, 0), castle(3, 8), 11},
		"the debt takes it all":    {castle(3, 8), castle(3, 5), 0},
		"the debt's last 2":        {castle(3, 2), castle(3, 0), 1},
	} {
		if got := growth(test.before, test.after); got != test.want {
			t.Errorf("%s: growth %d, want %d", name, got, test.want)
		}
	}
}

// Every castle's ledger in the web client's real bot games reconciles: the
// ledger follows the game's turn order exactly.
func TestTheLedgersOfRealGamesReconcile(t *testing.T) {
	paths, err := filepath.Glob(filepath.Join(webFixtures, "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	ledgers := 0
	for _, path := range paths {
		if filepath.Base(path) == "heading-arrows.json" {
			continue
		}
		demo, err := readDemoFile(path)
		if err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		game := newGame(filepath.Base(path), "demo file "+path, demo)
		for turn := 2; turn <= game.Turns(); turn++ {
			for castleID, ledger := range game.changes[turn-1].ledgers {
				ledgers++
				if !ledger.Reconciled {
					t.Errorf("%s turn %d castle %d: %+v", filepath.Base(path), turn, castleID, ledger)
				}
			}
		}
	}
	if ledgers < 50 {
		t.Fatalf("only %d ledgers in %d fixtures", ledgers, len(paths))
	}
	t.Logf("%d ledgers reconcile", ledgers)
}

func TestDiffTurnsListsEveryChange(t *testing.T) {
	client := connectWithFixture(t)
	diff := call[Diff](t, client, "diff_turns", map[string]any{"from_turn": 1, "to_turn": 6})
	want := map[string][]string{
		"captures": {"turn 3: castle 2, neutral → Blue, by group 0", "turn 4: castle 0, Blue → Red, by group 1", "turn 6: castle 2, Blue → Red, by group 3"},
		"sent": {"turn 2: Blue group 0, 6 soldiers, castle 0 → castle 2 (sent on turn 1)", "turn 2: Red group 1, 8 soldiers, castle 1 → castle 0 (sent on turn 1)",
			"turn 5: Red group 3, 4 soldiers, castle 1 → castle 2 (sent on turn 4)"},
		"arrivals": {"turn 3: Blue group 0, 6 soldiers, at castle 2: captured (garrison 5 → 1)", "turn 4: Red group 1, 8 soldiers, at castle 0: captured (garrison 7 → 1)",
			"turn 6: Red group 3, 3 soldiers, at castle 2: captured (garrison 2 → 1)"},
		"defeated": {"turn 5: Blue group 2, 1 soldiers, by Red near (1.4, 1.6)"},
		"upgrades": {"turn 4: castle 2 (Blue) speed to level 2"},
	}
	got := map[string][]string{"captures": diff.Captures, "sent": diff.GroupsSent, "arrivals": diff.Arrivals, "defeated": diff.GroupsDefeated, "upgrades": diff.Upgrades}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("diff = %+v,\nwant %+v", got, want)
	}
	if len(diff.CastleChanges) != 3 || diff.CastleChanges[2] != (CastleChange{CastleID: 2, OwnerFrom: "neutral", OwnerTo: "Red", SoldiersFrom: 5, SoldiersTo: 1, Upgrades: "speed 1 → 2"}) {
		t.Fatalf("castle changes = %+v", diff.CastleChanges)
	}
	// Castle 2 on turn 5 reconciles through the soldiers its note explains.
	if len(diff.Unreconciled) != 0 {
		t.Fatalf("unreconciled = %+v", diff.Unreconciled)
	}

	// One turn by default, and only one team's events with team.
	last := call[Diff](t, client, "diff_turns", map[string]any{"to_turn": 5, "team": "blue"})
	if last.FromTurn != 4 || len(last.GroupsDefeated) != 1 || len(last.GroupsSent) != 1 || len(last.Captures) != 0 {
		t.Fatalf("turn 5 for Blue = %+v", last)
	}
	if message := fails(t, client, "diff_turns", map[string]any{"from_turn": 3, "to_turn": 3}); !strings.Contains(message, "must come before") {
		t.Fatalf("message = %q", message)
	}
}

func TestGetTurnShowsTheBoardAndWhatMarchesOnEachCastle(t *testing.T) {
	client := connectWithFixture(t)
	view := call[TurnView](t, client, "get_turn", map[string]any{"turn": 2, "team": "1"})
	if view.GenerationID != 102 || view.Outcome != nil || len(view.Castles) != 1 || len(view.Groups) != 2 {
		t.Fatalf("turn 2 for Blue = %+v", view)
	}
	if castle := view.Castles[0]; castle.ID != 0 || castle.Soldiers != 5 || castle.IncomingHostile != 8 || castle.IncomingFriendly != 0 {
		t.Fatalf("castle 0 = %+v", castle)
	}
	if group := view.Groups[1]; group.ID != 1 || group.Team != "Red" || group.SentOnTurn != 1 || group.DistanceLeft != 6 {
		t.Fatalf("group 1 = %+v", group)
	}
	clashes := call[TurnView](t, client, "get_turn", map[string]any{"turn": 5})
	if len(clashes.Clashes) != 1 || !reflect.DeepEqual(clashes.Clashes[0].Participants, []string{"Blue group 2: 1 → 0", "Red group 3: 4 → 3"}) {
		t.Fatalf("turn 5 clashes = %+v", clashes.Clashes)
	}
	if castle := clashes.Castles[2]; !reflect.DeepEqual(castle.Upgrades, map[string]int{"speed": 2}) {
		t.Fatalf("castle 2 = %+v", castle)
	}
	end := call[TurnView](t, client, "get_turn", map[string]any{"turn": 6})
	if end.Outcome == nil {
		t.Fatal("the last turn has no outcome")
	}
	if message := fails(t, client, "get_turn", map[string]any{"turn": 7}); !strings.Contains(message, "turns 1 to 6") {
		t.Fatalf("message = %q", message)
	}
	if message := fails(t, client, "get_turn", map[string]any{"turn": 1, "team": "Green"}); !strings.Contains(message, "Blue (1), Red (2)") {
		t.Fatalf("message = %q", message)
	}
}

func TestGetLogsFiltersEveryTeamsLines(t *testing.T) {
	client := connectWithFixture(t)
	all := call[LogsView](t, client, "get_logs", map[string]any{"turn": 1})
	if all.Lines != 3 || len(all.Turns) != 1 || len(all.Turns[0].Players) != 2 {
		t.Fatalf("turn 1 = %+v", all)
	}
	red := call[LogsView](t, client, "get_logs", map[string]any{"turn": 1, "to_turn": 6, "team": "Red", "min_level": "info"})
	want := []TurnLogs{
		{Turn: 1, Players: []PlayerLine{{Player: "Sam", Team: "Red", Lines: []string{"info island_battles.player: attacking castle 0 with 8"}}}},
		{Turn: 2, Players: []PlayerLine{{Player: "Sam", Team: "Red", Lines: []string{"info island_battles.player: marching on castle 0"}}}},
		{Turn: 4, Players: []PlayerLine{{Player: "Sam", Team: "Red", Lines: []string{"info island_battles.player: attacking castle 2 with 4"}}}},
	}
	if !reflect.DeepEqual(red.Turns, want) || red.Note != "" {
		t.Fatalf("Red's lines = %+v", red)
	}
	found := call[LogsView](t, client, "get_logs", map[string]any{"turn": 1, "to_turn": 6, "contains": "CASTLE 2", "player": "alex"})
	if found.Lines != 2 {
		t.Fatalf("Alex's lines about castle 2 = %+v", found)
	}
	errors := call[LogsView](t, client, "get_logs", map[string]any{"turn": 1, "to_turn": 6, "min_level": "error"})
	if errors.Lines != 1 || errors.Turns[0].Turn != 5 {
		t.Fatalf("errors = %+v", errors)
	}
}

func TestCastleHistoryAndCapturesAndTimeline(t *testing.T) {
	client := connectWithFixture(t)
	history := call[CastleHistory](t, client, "castle_history", map[string]any{"castle_id": 2})
	turns := make([]int, 0, len(history.Turns))
	for _, row := range history.Turns {
		turns = append(turns, row.Turn)
	}
	if !reflect.DeepEqual(turns, []int{1, 3, 4, 5, 6}) || history.Turns[1].Team != "Blue" ||
		!reflect.DeepEqual(history.Turns[1].Events, []string{"Blue group 0 arrived with 6 soldiers from castle 0: captured (5 → 1)"}) {
		t.Fatalf("castle 2's history = %+v", history)
	}
	captures := call[CaptureList](t, client, "list_captures", map[string]any{"team": "Blue", "from_turn": 4})
	one, three := uint(1), uint(3)
	if want := []CaptureView{{Turn: 4, CastleID: 0, From: "Blue", To: "Red", SoldiersAfter: 1, ByGroup: &one}, {Turn: 6, CastleID: 2, From: "Blue", To: "Red", SoldiersAfter: 1, ByGroup: &three}}; !reflect.DeepEqual(captures.Captures, want) {
		t.Fatalf("captures = %+v", captures.Captures)
	}
	timeline := call[Timeline](t, client, "team_timeline", map[string]any{"step": 4})
	if timeline.Step != 4 || !reflect.DeepEqual(timeline.Teams, []string{"Blue", "Red"}) || len(timeline.Rows) != 3 || timeline.Rows[2].Turn != 6 ||
		timeline.Rows[2].Values[0] != "castles 0, castle soldiers 0, groups 0 with 0 soldiers" || timeline.Rows[0].Values[1] != "castles 1, castle soldiers 10, groups 0 with 0 soldiers" {
		t.Fatalf("timeline = %+v", timeline)
	}
}

func TestDemoFilesAreCheckedAndMayBePacked(t *testing.T) {
	raw, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	var packed bytes.Buffer
	writer := gzip.NewWriter(&packed)
	_, _ = writer.Write(raw)
	_ = writer.Close()
	write := func(name string, content []byte) string {
		path := filepath.Join(directory, name)
		if err := os.WriteFile(path, content, 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	client := connect(t, newStore(newLobbyClient("http://lobby.invalid", "http://game.invalid", "AI agent", http.DefaultClient, quietLogger()), quietLogger()))
	if message := fails(t, client, "game_overview", nil); !strings.Contains(message, "no game is loaded") {
		t.Fatalf("message = %q", message)
	}
	if overview := call[Overview](t, client, "load_demo", map[string]any{"path": write("packed.json.gz", packed.Bytes())}); overview.Turns != 6 {
		t.Fatalf("packed demo = %+v", overview)
	}
	older := strings.Replace(string(raw), `"schema_version": 2`, `"schema_version": 1`, 1)
	for name, content := range map[string]string{
		"not JSON":        "<html>",
		"a map file":      `{"map_name":"Islands","castles":[]}`,
		"an older format": older,
	} {
		message := fails(t, client, "load_demo", map[string]any{"path": write("bad.json", []byte(content))})
		if !strings.Contains(message, "demo") {
			t.Errorf("%s: message = %q", name, message)
		}
	}
	if message := fails(t, client, "load_demo", map[string]any{"path": write("older.json", []byte(older))}); !strings.Contains(message, "format version 1, and this server reads only version 2") {
		t.Fatalf("older format message = %q", message)
	}
	// Both games stay loaded, and game names one.
	if overview := call[Overview](t, client, "game_overview", map[string]any{"game": "packed.json.gz"}); !slices.Contains(overview.LoadedGames, "packed.json.gz") {
		t.Fatalf("loaded = %+v", overview.LoadedGames)
	}
}

// fakeLobby serves the lobby's and the game's routes that lobby mode reads.
type fakeLobby struct {
	mutex      sync.Mutex
	demo       []byte
	demoReads  int
	sessions   int
	codes      []string
	authorized []string
	// unwatched lists the lobbies a watch was removed from, with the
	// credential that removed it.
	unwatched []string
}

func (fake *fakeLobby) handler(t *testing.T) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /lobby/v1/sessions", func(response http.ResponseWriter, request *http.Request) {
		var body map[string]string
		_ = json.NewDecoder(request.Body).Decode(&body)
		fake.mutex.Lock()
		fake.sessions++
		fake.mutex.Unlock()
		if body["display_name"] != "AI agent" {
			t.Errorf("display name %q", body["display_name"])
		}
		_, _ = io.WriteString(response, `{"player_id":"p1","access_token":"guest-credential","expires_at":"2999-01-01T00:00:00Z"}`)
	})
	mux.HandleFunc("POST /lobby/v1/watches", func(response http.ResponseWriter, request *http.Request) {
		var body map[string]string
		_ = json.NewDecoder(request.Body).Decode(&body)
		fake.mutex.Lock()
		fake.codes = append(fake.codes, body["join_code"])
		fake.mutex.Unlock()
		if request.Header.Get("Authorization") != "Bearer guest-credential" || body["game_key"] != "island-battles" {
			t.Errorf("watch request %v %v", request.Header, body)
		}
		if body["join_code"] != "ABCD2345" {
			response.WriteHeader(http.StatusNotFound)
			_, _ = io.WriteString(response, `{"code":"not_found","detail":"lobby not found"}`)
			return
		}
		_, _ = io.WriteString(response, `{"game_key":"island-battles","lobby_id":"lobby-1","channel":"watch:island-battles:lobby-1","member":false}`)
	})
	mux.HandleFunc("POST /lobby/v1/player-tokens", func(response http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "Bearer guest-credential" {
			t.Errorf("token request %v", request.Header)
		}
		_, _ = io.WriteString(response, `{"token":"player-token","expires_at":"2999-01-01T00:00:00Z"}`)
	})
	mux.HandleFunc("DELETE /lobby/v1/games/{game_key}/lobbies/{lobby_id}/watch", func(response http.ResponseWriter, request *http.Request) {
		fake.mutex.Lock()
		fake.unwatched = append(fake.unwatched, request.PathValue("game_key")+"/"+request.PathValue("lobby_id")+" "+request.Header.Get("Authorization"))
		fake.mutex.Unlock()
		response.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("GET /game/from-lobby/{lobby_id}/watch/demo", func(response http.ResponseWriter, request *http.Request) {
		fake.mutex.Lock()
		defer fake.mutex.Unlock()
		fake.demoReads++
		fake.authorized = append(fake.authorized, request.Header.Get("Authorization"))
		if request.PathValue("lobby_id") != "lobby-1" {
			response.WriteHeader(http.StatusNotFound)
			return
		}
		response.Header().Set("Content-Type", "application/json")
		response.Header().Set("Content-Encoding", "gzip")
		writer := gzip.NewWriter(response)
		_, _ = writer.Write(fake.demo)
		_ = writer.Close()
	})
	return mux
}

// watchedWhileRunning turns the fixture into what a watcher reads of the
// game while it runs: no outcome, and no team's logs.
func watchedWhileRunning(t *testing.T, raw []byte) []byte {
	t.Helper()
	var running map[string]any
	if err := json.Unmarshal(raw, &running); err != nil {
		t.Fatal(err)
	}
	running["outcome"] = nil
	for _, entry := range running["logs"].([]any) {
		logs := entry.(map[string]any)
		logs["players"] = []any{}
		logs["hidden_team_colors"] = []any{1, 2}
	}
	encoded, err := json.Marshal(running)
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}

// The server stops watching the lobbies it watched when it stops, so the
// host's watcher list doesn't keep it and restarts don't fill the lobby's
// watcher limit.
func TestTheServerStopsWatchingWhenItStops(t *testing.T) {
	raw, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Fatal(err)
	}
	fake := &fakeLobby{demo: watchedWhileRunning(t, raw)}
	server := httptest.NewServer(fake.handler(t))
	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	serverTransport, _ := mcp.NewInMemoryTransports()
	stopped := make(chan error, 1)
	go func() {
		stopped <- run(ctx, options{lobbyCode: "ABCD2345", gameURL: server.URL, lobbyURL: server.URL, displayName: "AI agent"}, serverTransport, server.Client(), quietLogger())
	}()
	// Wait until the game is read, which follows the watch.
	for deadline := time.Now().Add(10 * time.Second); ; {
		fake.mutex.Lock()
		reads := fake.demoReads
		fake.mutex.Unlock()
		if reads > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the game was never read")
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	select {
	case err := <-stopped:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the server didn't stop")
	}
	fake.mutex.Lock()
	defer fake.mutex.Unlock()
	if !reflect.DeepEqual(fake.unwatched, []string{"island-battles/lobby-1 Bearer guest-credential"}) {
		t.Fatalf("unwatched = %q", fake.unwatched)
	}
}

func TestLobbyModeReadsTheGameAsAWatcher(t *testing.T) {
	raw, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Fatal(err)
	}
	runningRaw := watchedWhileRunning(t, raw)
	fake := &fakeLobby{demo: runningRaw}
	server := httptest.NewServer(fake.handler(t))
	defer server.Close()
	games := newStore(newLobbyClient(server.URL+"/", server.URL, "AI agent", server.Client(), quietLogger()), quietLogger())
	client := connect(t, games)

	overview := call[Overview](t, client, "load_lobby", map[string]any{"code": " abcd2345 "})
	if overview.Game != "ABCD2345" || !overview.Running || overview.Turns != 6 || !strings.Contains(overview.Source, "lobby code ABCD2345") {
		t.Fatalf("overview = %+v", overview)
	}
	if !reflect.DeepEqual(fake.authorized, []string{"Bearer player-token"}) {
		t.Fatalf("demo reads authorized with %q", fake.authorized)
	}
	// A watcher reads no logs while the game runs, and the tools say why.
	if !strings.HasPrefix(overview.Logs, "No bot logs: the game is still running") {
		t.Fatalf("overview logs = %q", overview.Logs)
	}
	logs := call[LogsView](t, client, "get_logs", map[string]any{"turn": 1, "to_turn": 6})
	if logs.Lines != 0 || !strings.Contains(logs.Note, "The lines of Blue, Red are not in this copy: the game is still running") || !strings.Contains(logs.Note, "Turn 6 is still open") {
		t.Fatalf("logs = %+v", logs)
	}
	explanation := call[CastleExplanation](t, client, "explain_castle", map[string]any{"castle_id": 2, "turn": 4})
	if len(explanation.Logs) == 0 {
		t.Fatal("explain_castle named no team's logs")
	}
	for _, team := range explanation.Logs {
		if len(team.Lines) != 0 || !strings.Contains(team.Hidden, "still running") {
			t.Fatalf("explained logs = %+v", explanation.Logs)
		}
	}
	// A running game is read again once its copy is old.
	games.mutex.Lock()
	games.games["ABCD2345"].LoadedAt = games.games["ABCD2345"].LoadedAt.Add(-liveRefresh - time.Second)
	games.mutex.Unlock()
	call[Overview](t, client, "game_overview", nil)
	if fake.demoReads != 2 || fake.sessions != 1 {
		t.Fatalf("demo reads %d, sessions %d", fake.demoReads, fake.sessions)
	}
	if message := fails(t, client, "load_lobby", map[string]any{"code": "ZZZZ2345"}); !strings.Contains(message, "no Island Battles lobby has the code ZZZZ2345") {
		t.Fatalf("message = %q", message)
	}
}

// A demo saved during the game holds only its saver's team's logs, and the
// tools say so instead of suggesting the other teams logged nothing.
func TestADemoSavedDuringTheGameSaysWhoseLogsItHolds(t *testing.T) {
	raw, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Fatal(err)
	}
	var demo map[string]any
	if err := json.Unmarshal(raw, &demo); err != nil {
		t.Fatal(err)
	}
	demo["outcome"] = nil
	for _, entry := range demo["logs"].([]any) {
		logs := entry.(map[string]any)
		logs["players"] = slices.DeleteFunc(logs["players"].([]any), func(player any) bool { return player.(map[string]any)["team_color"] != float64(1) })
		logs["hidden_team_colors"] = []any{2}
	}
	encoded, err := json.Marshal(demo)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "saved-early.json")
	if err := os.WriteFile(path, encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	client := connect(t, newStore(newLobbyClient("http://lobby.invalid", "http://game.invalid", "AI agent", http.DefaultClient, quietLogger()), quietLogger()))
	overview := call[Overview](t, client, "load_demo", map[string]any{"path": path})
	if !strings.HasPrefix(overview.Logs, "No bot logs of Red: this demo was saved before the game ended") {
		t.Fatalf("overview logs = %q", overview.Logs)
	}
	logs := call[LogsView](t, client, "get_logs", map[string]any{"turn": 1, "to_turn": 5})
	if logs.Lines == 0 || !strings.Contains(logs.Note, "The lines of Red are not in this copy: this demo was saved before the game ended") {
		t.Fatalf("logs = %+v", logs)
	}
	for _, turn := range logs.Turns {
		for _, player := range turn.Players {
			if player.Team != "Blue" {
				t.Fatalf("lines of %s", player.Team)
			}
		}
	}
}
