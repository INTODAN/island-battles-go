package services_test

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/INTODAN/mpl/domain"
	"github.com/google/uuid"
	"gorm.io/gorm/clause"
	"islandBattles/backend/dal"
	"islandBattles/backend/internal/testutils"
	"islandBattles/backend/models"
	"islandBattles/backend/services"
)

// newLogFixture starts a game with Alice on Blue and Bob on Red.
// Members are listed, so the game copies their display names. The state is
// Alice's.
func newLogFixture(t *testing.T) turnFixture {
	t.Helper()
	database := testutils.InitTestDB(t)
	lobbyID, blue, red := uuid.NewString(), uuid.NewString(), uuid.NewString()
	alice, bob := domain.Principal{PlayerID: uuid.NewString()}, domain.Principal{PlayerID: uuid.NewString()}
	publisher := &testutils.RecordedPublisher{}
	if _, err := services.RunLobbyGame(context.Background(), database, publisher, nil, domain.Snapshot{
		Lobby:       domain.Lobby{ID: lobbyID, GameKey: "island-battles", Phase: domain.PhaseStarted},
		Teams:       []domain.Team{{ID: blue, Order: 1, Name: "Blue"}, {ID: red, Order: 2, Name: "Red"}},
		StartRoster: domain.StartRoster{{PlayerID: alice.PlayerID, TeamID: blue}, {PlayerID: bob.PlayerID, TeamID: red}},
		Members:     []domain.Member{{PlayerID: alice.PlayerID, DisplayName: "Alice"}, {PlayerID: bob.PlayerID, DisplayName: "Bob"}},
	}); err != nil {
		t.Fatal(err)
	}
	service := &services.GenerationService{Database: database, Publisher: publisher}
	state, err := service.Current(context.Background(), alice, lobbyID)
	if err != nil {
		t.Fatal(err)
	}
	return turnFixture{database, publisher, service, []domain.Principal{alice, bob}, state}
}

func logLine(level models.GenerationLogLevel, message string) models.GenerationLogLine {
	return models.GenerationLogLine{Level: level, Logger: "island_battles.player", Message: message}
}

func logLines(count int, prefix string) []models.GenerationLogLine {
	lines := make([]models.GenerationLogLine, count)
	for index := range lines {
		lines[index] = logLine(models.GenerationLogInfo, prefix+" "+strings.Repeat("x", index%7+1))
	}
	return lines
}

func withLogs(request models.RegisterGenerationCommands, lines ...models.GenerationLogLine) models.RegisterGenerationCommands {
	request.Logs = lines
	return request
}

func assertGameLogLimit(t *testing.T, err error) {
	t.Helper()
	var rejection *services.GenerationError
	if !errors.As(err, &rejection) || rejection.Status != 422 || rejection.Type != services.GameLogLimitProblemType {
		t.Fatalf("error=%v, want a 422 of type %s", err, services.GameLogLimitProblemType)
	}
}

func readLogs(t *testing.T, fixture turnFixture, player int, generationID uint) models.GenerationLogs {
	t.Helper()
	logs, err := fixture.service.Logs(context.Background(), fixture.players[player], fixture.state.LobbyID, generationID)
	if err != nil {
		t.Fatal(err)
	}
	return logs
}

func countLogRows(t *testing.T, fixture turnFixture) int64 {
	t.Helper()
	var count int64
	if err := fixture.database.Model(&models.GenerationLog{}).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	return count
}

func TestGameLogLinesAreStoredInOrderWithTheirGeneration(t *testing.T) {
	fixture := newLogFixture(t)
	ctx := context.Background()
	alice, bob := fixture.players[0], fixture.players[1]
	state := fixture.state
	var origin, target uint
	for _, castle := range state.Generation.Castles {
		if castle.TeamColor == state.TeamColor {
			origin = castle.ID
		} else {
			target = castle.ID
		}
	}
	// Logs travel with commands, alone, or with the completion, and a retry
	// stores nothing twice.
	first := withLogs(batch(state, false, send(origin, target, 1)), logLine(models.GenerationLogDebug, "planning"), logLine(models.GenerationLogInfo, "attacking castle 1"))
	for range 2 {
		if _, err := fixture.service.Register(ctx, alice, first); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := fixture.service.Register(ctx, alice, withLogs(batch(state, false), logLine(models.GenerationLogWarning, "low on soldiers"))); err != nil {
		t.Fatal(err)
	}
	changed := withLogs(first, logLine(models.GenerationLogInfo, "something else"))
	if _, err := fixture.service.Register(ctx, alice, changed); err == nil {
		t.Fatal("a reused request ID with other logs was accepted")
	} else {
		assertTurnStatus(t, err, 409)
	}
	if _, err := fixture.service.Register(ctx, alice, withLogs(batch(state, true), logLine(models.GenerationLogError, "done"))); err != nil {
		t.Fatal(err)
	}
	// A sealed turn takes no more lines.
	assertTurnStatus(t, func() error {
		_, err := fixture.service.Register(ctx, alice, withLogs(batch(state, false), logLine(models.GenerationLogInfo, "late")))
		return err
	}(), 409)
	if _, err := fixture.service.Register(ctx, bob, withLogs(batch(state, true), logLine(models.GenerationLogInfo, "bob's secret plan"))); err != nil {
		t.Fatal(err)
	}
	// The turn advanced, so a batch for the old generation is stale.
	assertTurnStatus(t, func() error {
		_, err := fixture.service.Register(ctx, bob, withLogs(batch(state, false), logLine(models.GenerationLogInfo, "stale")))
		return err
	}(), 409)

	aliceLines := []models.GenerationLogLine{logLine(models.GenerationLogDebug, "planning"), logLine(models.GenerationLogInfo, "attacking castle 1"),
		logLine(models.GenerationLogWarning, "low on soldiers"), logLine(models.GenerationLogError, "done")}
	want := models.GenerationLogs{GenerationID: state.GenerationID, HiddenTeamColors: []uint{2}, Players: []models.PlayerGenerationLogs{
		{DisplayName: "Alice", TeamColor: 1, Lines: aliceLines},
	}}
	// While the game runs, each team sees only its own lines.
	if logs := readLogs(t, fixture, 0, state.GenerationID); !reflect.DeepEqual(logs, want) {
		t.Fatalf("Alice's view = %+v, want %+v", logs, want)
	}
	bobView := models.GenerationLogs{GenerationID: state.GenerationID, HiddenTeamColors: []uint{1}, Players: []models.PlayerGenerationLogs{
		{DisplayName: "Bob", TeamColor: 2, Lines: []models.GenerationLogLine{logLine(models.GenerationLogInfo, "bob's secret plan")}},
	}}
	if logs := readLogs(t, fixture, 1, state.GenerationID); !reflect.DeepEqual(logs, bobView) {
		t.Fatalf("Bob's view = %+v, want %+v", logs, bobView)
	}
	// The next generation has no lines yet, and every visible player is listed.
	next, err := fixture.service.Current(ctx, alice, state.LobbyID)
	if err != nil {
		t.Fatal(err)
	}
	empty := readLogs(t, fixture, 0, next.GenerationID)
	if len(empty.Players) != 1 || len(empty.Players[0].Lines) != 0 || !reflect.DeepEqual(empty.HiddenTeamColors, []uint{2}) {
		t.Fatalf("next generation = %+v", empty)
	}

	// Once the game has an outcome, everyone sees every team.
	game, err := dal.FindGame(fixture.database, state.GameID)
	if err != nil {
		t.Fatal(err)
	}
	if err := dal.SaveGameOutcome(fixture.database, game, models.GameOutcome{Reason: models.GameOutcomeGenerationCap}); err != nil {
		t.Fatal(err)
	}
	ended := readLogs(t, fixture, 1, state.GenerationID)
	everyone := models.GenerationLogs{GenerationID: state.GenerationID, HiddenTeamColors: []uint{}, Players: append(want.Players, bobView.Players...)}
	if !reflect.DeepEqual(ended, everyone) {
		t.Fatalf("Bob's view after the end = %+v, want %+v", ended, everyone)
	}
	// Players are named, never identified.
	raw, err := json.Marshal(ended)
	if err != nil {
		t.Fatal(err)
	}
	for _, player := range fixture.players {
		if strings.Contains(string(raw), player.PlayerID) {
			t.Fatalf("the logs name a player ID: %s", raw)
		}
	}
	var sequences []uint
	if err := fixture.database.Model(&models.GenerationLog{}).Where(&models.GenerationLog{GenerationID: state.GenerationID, PlayerID: alice.PlayerID}).
		Order(clause.OrderByColumn{Column: clause.Column{Name: "sequence"}}).Pluck("sequence", &sequences).Error; err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(sequences, []uint{1, 2, 3, 4}) {
		t.Fatalf("Alice's sequences = %v", sequences)
	}
}

func TestGameLogLimitsRejectTheWholeBatch(t *testing.T) {
	fixture := newLogFixture(t)
	ctx := context.Background()
	alice, bob := fixture.players[0], fixture.players[1]
	state := fixture.state
	register := func(player domain.Principal, request models.RegisterGenerationCommands) error {
		_, err := fixture.service.Register(ctx, player, request)
		return err
	}
	assertGameLogLimit(t, register(alice, withLogs(batch(state, false), logLines(101, "too many")...)))
	if err := register(alice, withLogs(batch(state, false), logLines(60, "first")...)); err != nil {
		t.Fatal(err)
	}
	// Past the limit across batches: nothing of the batch is stored, and the
	// turn stays open.
	assertGameLogLimit(t, register(alice, withLogs(batch(state, true), logLines(41, "second")...)))
	if current, err := fixture.service.Current(ctx, alice, state.LobbyID); err != nil || current.Submitted {
		t.Fatalf("turn after a rejected batch: submitted=%v, %v", current.Submitted, err)
	}
	if err := register(alice, withLogs(batch(state, true), logLines(40, "second")...)); err != nil {
		t.Fatal(err)
	}
	if lines := readLogs(t, fixture, 0, state.GenerationID).Players[0].Lines; len(lines) != 100 || lines[60].Message != "second x" {
		t.Fatalf("Alice stored %d lines", len(lines))
	}

	// Lines are measured in code points: 500 accented letters fit, 501 don't.
	if err := register(bob, withLogs(batch(state, false), logLine(models.GenerationLogInfo, strings.Repeat("é", 500)))); err != nil {
		t.Fatal(err)
	}
	assertGameLogLimit(t, register(bob, withLogs(batch(state, false), logLine(models.GenerationLogInfo, strings.Repeat("é", 501)))))
	for name, line := range map[string]models.GenerationLogLine{
		"unknown level":   {Level: "critical", Logger: "island_battles.player", Message: "boom"},
		"missing level":   {Logger: "island_battles.player", Message: "boom"},
		"empty message":   logLine(models.GenerationLogInfo, ""),
		"long logger":     {Level: models.GenerationLogInfo, Logger: strings.Repeat("l", 101), Message: "boom"},
		"NUL in message":  logLine(models.GenerationLogInfo, "a\x00b"),
		"NUL in a logger": {Level: models.GenerationLogInfo, Logger: "a\x00b", Message: "boom"},
	} {
		err := register(bob, withLogs(batch(state, false), line))
		var rejection *services.GenerationError
		if !errors.As(err, &rejection) || rejection.Status != 422 || rejection.Type != "" {
			t.Errorf("%s: error=%v, want a plain 422", name, err)
		}
	}
	if rows := countLogRows(t, fixture); rows != 101 {
		t.Fatalf("stored %d lines, want 101", rows)
	}
}

func TestGameLogsNeedTheRosterAndAGenerationOfTheGame(t *testing.T) {
	fixture := newLogFixture(t)
	ctx := context.Background()
	alice := fixture.players[0]
	// Another game in the same database, whose generation Alice can't read
	// through her lobby.
	otherLobby, otherTeam, otherPlayer := uuid.NewString(), uuid.NewString(), domain.Principal{PlayerID: uuid.NewString()}
	if _, err := services.RunLobbyGame(ctx, fixture.database, fixture.publisher, nil, domain.Snapshot{
		Lobby: domain.Lobby{ID: otherLobby, GameKey: "island-battles", Phase: domain.PhaseStarted},
		Teams: []domain.Team{{ID: otherTeam, Order: 1}}, StartRoster: domain.StartRoster{{PlayerID: otherPlayer.PlayerID, TeamID: otherTeam}},
	}); err != nil {
		t.Fatal(err)
	}
	otherState, err := fixture.service.Current(ctx, otherPlayer, otherLobby)
	if err != nil {
		t.Fatal(err)
	}
	for name, read := range map[string]func() error{
		"another game's generation": func() error {
			_, err := fixture.service.Logs(ctx, alice, fixture.state.LobbyID, otherState.GenerationID)
			return err
		},
		"an unknown generation": func() error {
			_, err := fixture.service.Logs(ctx, alice, fixture.state.LobbyID, otherState.GenerationID+100)
			return err
		},
		"another game's player": func() error {
			_, err := fixture.service.Logs(ctx, otherPlayer, fixture.state.LobbyID, fixture.state.GenerationID)
			return err
		},
	} {
		var rejection *services.GenerationError
		if err := read(); !errors.As(err, &rejection) || rejection.Status != 404 {
			t.Errorf("%s: error=%v, want 404", name, err)
		}
	}
	if _, err := fixture.service.Logs(ctx, alice, uuid.NewString(), fixture.state.GenerationID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("unknown lobby error = %v", err)
	}
	// The lobby listed no member for the other game's player, so the game
	// names them by roster position.
	logs, err := fixture.service.Logs(ctx, otherPlayer, otherLobby, otherState.GenerationID)
	if err != nil {
		t.Fatal(err)
	}
	if len(logs.Players) != 1 || logs.Players[0].DisplayName != "Player 1" || len(logs.HiddenTeamColors) != 0 {
		t.Fatalf("unnamed player's logs = %+v", logs)
	}
	// The generation is checked only for roster players.
	if _, err := fixture.service.Logs(ctx, otherPlayer, fixture.state.LobbyID, 0); err == nil {
		t.Fatal("an outsider read generation 0")
	} else {
		assertTurnStatus(t, err, 404)
	}
	if _, err := fixture.service.Logs(ctx, alice, fixture.state.LobbyID, 0); err == nil {
		t.Fatal("Alice read generation 0")
	} else {
		assertTurnStatus(t, err, 422)
	}
}

func TestStatesCountTheCallersStoredLogLines(t *testing.T) {
	fixture := newLogFixture(t)
	ctx := context.Background()
	alice, bob := fixture.players[0], fixture.players[1]
	state := fixture.state
	if state.LogLinesStored != 0 {
		t.Fatalf("a fresh generation has %d stored lines", state.LogLinesStored)
	}
	first := withLogs(batch(state, false), logLines(3, "first")...)
	registered, err := fixture.service.Register(ctx, alice, first)
	if err != nil {
		t.Fatal(err)
	}
	if registered.LogLinesStored != 3 {
		t.Fatalf("after a batch: %d stored lines, want 3", registered.LogLinesStored)
	}
	// A retry answers the same count, and so does a read, which is what a
	// restarted client counts from.
	if retried, err := fixture.service.Register(ctx, alice, first); err != nil || retried.LogLinesStored != 3 {
		t.Fatalf("retry: %d stored lines, %v", retried.LogLinesStored, err)
	}
	if read, err := fixture.service.Current(ctx, alice, state.LobbyID); err != nil || read.LogLinesStored != 3 {
		t.Fatalf("read: %d stored lines, %v", read.LogLinesStored, err)
	}
	// Each player counts only their own lines.
	if read, err := fixture.service.Current(ctx, bob, state.LobbyID); err != nil || read.LogLinesStored != 0 {
		t.Fatalf("Bob's read: %d stored lines, %v", read.LogLinesStored, err)
	}
	for _, player := range []domain.Principal{alice, bob} {
		if _, err := fixture.service.Register(ctx, player, batch(state, true)); err != nil {
			t.Fatal(err)
		}
	}
	// The next generation starts from zero, in reads and in pushed events.
	if read, err := fixture.service.Current(ctx, alice, state.LobbyID); err != nil || read.GenerationID == state.GenerationID || read.LogLinesStored != 0 {
		t.Fatalf("next generation: %+v, %v", read, err)
	}
	for _, publication := range fixture.publisher.Publications() {
		var event models.GenerationEvent
		if err := json.Unmarshal(publication.Data, &event); err != nil || event.State.LogLinesStored != 0 {
			t.Fatalf("event %s: %v", publication.Data, err)
		}
	}
}

// Concurrent batches of one player can't pass the 100-line limit together,
// and a request sent several times at once stores its lines once. From the
// review of PR #86.
func TestConcurrentGameLogBatchesKeepTheLimit(t *testing.T) {
	fixture := newLogFixture(t)
	ctx := context.Background()
	alice := fixture.players[0]
	state := fixture.state
	const workers = 6
	var group sync.WaitGroup
	errs := make([]error, workers)
	for index := range workers {
		group.Go(func() {
			_, errs[index] = fixture.service.Register(ctx, alice, withLogs(batch(state, false), logLines(30, "concurrent")...))
		})
	}
	group.Wait()
	accepted := 0
	for _, err := range errs {
		if err == nil {
			accepted++
		} else {
			assertGameLogLimit(t, err)
		}
	}
	if rows := countLogRows(t, fixture); accepted != 3 || rows != 90 {
		t.Fatalf("accepted %d batches, stored %d rows; errors %v", accepted, rows, errs)
	}
	retried := withLogs(batch(state, false), logLines(10, "retried")...)
	for index := range workers {
		group.Go(func() { _, errs[index] = fixture.service.Register(ctx, alice, retried) })
	}
	group.Wait()
	for _, err := range errs {
		if err != nil {
			t.Fatalf("retry error %v", err)
		}
	}
	if rows := countLogRows(t, fixture); rows != 100 {
		t.Fatalf("stored %d rows after a retried request, want 100", rows)
	}
	// One more line is refused whole, with the completion it carries.
	assertGameLogLimit(t, func() error {
		_, err := fixture.service.Register(ctx, alice, withLogs(batch(state, true), logLine(models.GenerationLogInfo, "restarted")))
		return err
	}())
	if read, err := fixture.service.Current(ctx, alice, state.LobbyID); err != nil || read.Submitted || read.LogLinesStored != 100 {
		t.Fatalf("after the refused completion: %+v, %v", read, err)
	}
}
