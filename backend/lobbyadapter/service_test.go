package lobbyadapter_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"sync"
	"testing"

	"islandBattles/backend/dal"
	"islandBattles/backend/internal/testutils"
	"islandBattles/backend/lobbyadapter"
	"islandBattles/backend/models"
	"islandBattles/lobby/application"
	"islandBattles/lobby/domain"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type snapshotApplication struct {
	snapshot domain.Snapshot
	err      error
}

func (application snapshotApplication) Snapshot(context.Context, domain.Principal, domain.Scope) (domain.Snapshot, error) {
	return application.snapshot, application.err
}
func TestBindStartedLobbyCreatesStableTeamMap(t *testing.T) {
	db := testutils.InitTestDB(t)
	principal, scope, snapshot := startedSnapshot()
	service := newGameBinding(t, db, snapshot)
	result, err := service.Bind(context.Background(), principal, scope)
	if err != nil {
		t.Fatal(err)
	}
	if result.GameID == 0 || result.LobbyID != scope.LobbyID || len(result.Teams) != 2 {
		t.Fatalf("binding result = %#v", result)
	}
	for index, binding := range result.Teams {
		if binding.TeamOrder != index+1 || binding.TeamColor != uint(index+1) {
			t.Fatalf("binding %d = %#v", index, binding)
		}
	}
	var game models.Game
	if err := db.First(&game, result.GameID).Error; err != nil {
		t.Fatal(err)
	}
	if game.LobbyID == nil || *game.LobbyID != scope.LobbyID {
		t.Fatalf("game lobby link = %#v", game.LobbyID)
	}
	var records []models.TeamBindingRecord
	if err := db.Where(&models.TeamBindingRecord{GameID: result.GameID}).Order("team_order").Find(&records).Error; err != nil {
		t.Fatal(err)
	}
	if len(records) != 2 || records[0].TeamColor == 0 || records[1].TeamColor == 0 {
		t.Fatalf("persisted mappings = %#v", records)
	}
}

func TestBindLobbyIsIdempotent(t *testing.T) {
	db := testutils.InitTestDB(t)
	principal, scope, snapshot := startedSnapshot()
	service := newGameBinding(t, db, snapshot)
	first, err := service.Bind(context.Background(), principal, scope)
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.Bind(context.Background(), principal, scope)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("replay changed result: %#v != %#v", first, second)
	}
	assertBindingCounts(t, db, scope.LobbyID, 1, 2)
}

func TestBindLobbyConcurrentRequests(t *testing.T) {
	db := testutils.InitTestDB(t)
	principal, scope, snapshot := startedSnapshot()
	service := newGameBinding(t, db, snapshot)
	start := make(chan struct{})
	results := make(chan models.BindingResult, 2)
	errors := make(chan error, 2)
	var group sync.WaitGroup
	for range 2 {
		group.Add(1)
		go func() {
			defer group.Done()
			<-start
			result, err := service.Bind(context.Background(), principal, scope)
			if err != nil {
				errors <- err
				return
			}
			results <- result
		}()
	}
	close(start)
	group.Wait()
	close(results)
	close(errors)
	for err := range errors {
		t.Fatal(err)
	}
	var first uint
	for result := range results {
		if first == 0 {
			first = result.GameID
		} else if first != result.GameID {
			t.Fatalf("concurrent game IDs = %d and %d", first, result.GameID)
		}
	}
	assertBindingCounts(t, db, scope.LobbyID, 1, 2)
}

func TestBindLobbyRequiresHostAndStartedIslandScope(t *testing.T) {
	db := testutils.InitTestDB(t)
	principal, scope, snapshot := startedSnapshot()
	nonHost := principal
	nonHost.PlayerID = uuid.NewString()
	service := newGameBinding(t, db, snapshot)
	if _, err := service.Bind(context.Background(), nonHost, scope); !errors.Is(err, domain.ErrNotHost) {
		t.Fatalf("nonhost err = %v", err)
	}
	assertBindingCounts(t, db, scope.LobbyID, 0, 0)
	waiting := snapshot
	waiting.Lobby.Phase = domain.PhaseWaiting
	if _, err := newGameBinding(t, db, waiting).Bind(context.Background(), principal, scope); !errors.Is(err, domain.ErrPhaseConflict) {
		t.Fatalf("waiting err = %v", err)
	}
	otherScope := scope
	otherScope.GameKey = "quiz-demo"
	if _, err := service.Bind(context.Background(), principal, otherScope); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("other game err = %v", err)
	}
	assertBindingCounts(t, db, scope.LobbyID, 0, 0)
}

func TestBindRunsAutomaticBattleAndSavesReplay(t *testing.T) {
	db := testutils.InitTestDB(t)
	principal, scope, snapshot := startedSnapshot()
	result, err := newGameBinding(t, db, snapshot).Bind(context.Background(), principal, scope)
	if err != nil {
		t.Fatal(err)
	}
	var generations int64
	if err := db.Model(&models.Generation{}).Count(&generations).Error; err != nil {
		t.Fatal(err)
	}
	if generations < 2 || generations > 61 {
		t.Fatalf("automatic battle created %d generations", generations)
	}
	game, err := dal.GetGameByID(db, result.GameID)
	if err != nil {
		t.Fatal(err)
	}
	if len(game.Generations[0].Map.Castles) != 2 {
		t.Fatal("missing player castles")
	}
	var replay struct {
		Teams []struct {
			Players []string `json:"players"`
		} `json:"teams"`
		Frames []struct {
			Groups []struct {
				Color       uint    `json:"color"`
				Soldiers    uint    `json:"soldiers"`
				X           float64 `json:"x"`
				ClashRadius float64 `json:"clash_radius"`
			} `json:"groups"`
		} `json:"frames"`
		Outcome string `json:"outcome"`
	}
	if err := json.Unmarshal(game.Replay, &replay); err != nil {
		t.Fatal(err)
	}
	if int64(len(replay.Frames)) != generations || replay.Outcome == "" || len(replay.Teams) != 2 || replay.Teams[0].Players[0] != "Host" || replay.Teams[1].Players[0] != "Guest" {
		t.Fatalf("incomplete replay: %+v", replay)
	}
	moved := false
	for _, frame := range replay.Frames {
		moved = moved || len(frame.Groups) > 0
	}
	if !moved {
		t.Fatal("automatic battle never sent troops")
	}
	// The first opposing groups (15 Blue vs 16 Red) meet during turn 6.
	// Neither group has less than half the other, so the surviving Red soldier
	// stops at the midpoint between their contact positions for this turn.
	if len(replay.Frames) <= 6 || len(replay.Frames[5].Groups) != 2 || len(replay.Frames[6].Groups) != 1 {
		t.Fatal("the first clash was not reflected in the replay")
	}
	survivor := replay.Frames[6].Groups[0]
	if survivor.Color != 2 || survivor.Soldiers != 1 || survivor.X != 0.75 || survivor.ClashRadius != 0.25 {
		t.Fatalf("unexpected first clash survivor: %+v", survivor)
	}
	if _, err := newGameBinding(t, db, snapshot).Bind(context.Background(), principal, scope); err != nil {
		t.Fatal(err)
	}
	reloaded, err := dal.GetGameByID(db, result.GameID)
	if err != nil {
		t.Fatal(err)
	}
	if len(reloaded.Generations) != len(game.Generations) || !bytes.Equal(reloaded.Replay, game.Replay) {
		t.Fatal("retry reran the battle")
	}
}

func startedSnapshot() (domain.Principal, domain.Scope, domain.Snapshot) {
	playerID, sessionID, lobbyID := uuid.NewString(), uuid.NewString(), uuid.NewString()
	host := playerID
	teamOne, teamTwo := uuid.NewString(), uuid.NewString()
	scope := domain.Scope{GameKey: "island-battles", LobbyID: lobbyID}
	guestID := uuid.NewString()
	snapshot := domain.Snapshot{Lobby: domain.Lobby{ID: lobbyID, GameKey: scope.GameKey, Phase: domain.PhaseStarted, HostPlayerID: &host}, Teams: []domain.Team{{ID: teamOne, LobbyID: lobbyID, Name: "Blue", Capacity: 4, Order: 1}, {ID: teamTwo, LobbyID: lobbyID, Name: "Red", Capacity: 4, Order: 2}}, StartRoster: domain.StartRoster{{PlayerID: playerID, TeamID: teamOne, MemberID: uuid.NewString(), TeamOrder: 1}, {PlayerID: guestID, TeamID: teamTwo, MemberID: uuid.NewString(), TeamOrder: 2}}, Members: []domain.Member{{PlayerID: playerID, DisplayName: "Host"}, {PlayerID: guestID, DisplayName: "Guest"}}}
	return domain.Principal{PlayerID: playerID, SessionID: sessionID, DisplayName: "Host"}, scope, snapshot
}

func newGameBinding(t *testing.T, db *gorm.DB, snapshot domain.Snapshot) *application.GameBinding[models.BindingResult] {
	t.Helper()
	service, err := lobbyadapter.NewService(db, snapshotApplication{snapshot: snapshot})
	if err != nil {
		t.Fatal(err)
	}
	return service
}

func assertBindingCounts(t *testing.T, db *gorm.DB, lobbyID string, games, bindings int64) {
	t.Helper()
	var gameCount, bindingCount int64
	if err := db.Model(&models.Game{}).Where(&models.Game{LobbyID: &lobbyID}).Count(&gameCount).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&models.TeamBindingRecord{}).Count(&bindingCount).Error; err != nil {
		t.Fatal(err)
	}
	if gameCount != games || bindingCount != bindings {
		t.Fatalf("counts games=%d bindings=%d, want games=%d bindings=%d", gameCount, bindingCount, games, bindings)
	}
}

func TestBindMappingFailureRollsBackGame(t *testing.T) {
	db := testutils.InitTestDB(t)
	principal, scope, snapshot := startedSnapshot()
	failure := errors.New("injected mapping failure")
	callback := "test:fail-team-bindings"
	if err := db.Callback().Create().Before("gorm:create").Register(callback, func(transaction *gorm.DB) {
		if transaction.Statement.Table == "island_game_team_bindings" {
			transaction.AddError(failure)
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Callback().Create().Remove(callback) })
	if result, err := newGameBinding(t, db, snapshot).Bind(context.Background(), principal, scope); !errors.Is(err, failure) || result.GameID != 0 {
		t.Fatalf("result=%#v error=%v", result, err)
	}
	assertBindingCounts(t, db, scope.LobbyID, 0, 0)
}

func TestBindRejectsIncompletePersistedMapping(t *testing.T) {
	db := testutils.InitTestDB(t)
	principal, scope, snapshot := startedSnapshot()
	service := newGameBinding(t, db, snapshot)
	result, err := service.Bind(context.Background(), principal, scope)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Where(&models.TeamBindingRecord{GameID: result.GameID, TeamID: result.Teams[0].TeamID}).Delete(&models.TeamBindingRecord{}).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := service.Bind(context.Background(), principal, scope); !errors.Is(err, domain.ErrUnavailable) {
		t.Fatalf("incomplete mapping error=%v", err)
	}
	assertBindingCounts(t, db, scope.LobbyID, 1, 1)
}

func TestFailedSimulationRollsBackAndCanBeRetried(t *testing.T) {
	database := testutils.InitTestDB(t)
	principal, scope, snapshot := startedSnapshot()
	service := newGameBinding(t, database, snapshot)
	failure := errors.New("injected generation failure")
	created := 0
	callback := "test:fail-simulation"
	if err := database.Callback().Create().Before("gorm:create").Register(callback, func(transaction *gorm.DB) {
		if transaction.Statement.Table == "generations" {
			created++
			if created == 3 {
				transaction.AddError(failure)
			}
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Callback().Create().Remove(callback) })
	if _, err := service.Bind(context.Background(), principal, scope); !errors.Is(err, failure) {
		t.Fatalf("unexpected failure: %v", err)
	}
	var generations int64
	if err := database.Model(&models.Generation{}).Count(&generations).Error; err != nil {
		t.Fatal(err)
	}
	if generations != 0 {
		t.Fatalf("left %d partial generations", generations)
	}
	replay, err := dal.GetLobbyGameReplay(database, scope.LobbyID)
	if err != nil || len(replay) != 0 {
		t.Fatalf("partial replay: %s; error: %v", replay, err)
	}
	if _, err := service.Bind(context.Background(), principal, scope); err != nil {
		t.Fatal(err)
	}
	replay, err = dal.GetLobbyGameReplay(database, scope.LobbyID)
	if err != nil || len(replay) == 0 {
		t.Fatalf("retry did not save replay: %v", err)
	}
	assertBindingCounts(t, database, scope.LobbyID, 1, 2)
}
