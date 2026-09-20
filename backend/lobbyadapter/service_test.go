package lobbyadapter_test

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"

	"islandBattles/backend/internal/testutils"
	"islandBattles/backend/lobbyadapter"
	"islandBattles/backend/models"
	"islandBattles/lobby"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type snapshotApplication struct {
	snapshot lobby.Snapshot
	err      error
}

func (application snapshotApplication) Snapshot(context.Context, lobby.Principal, lobby.Scope) (lobby.Snapshot, error) {
	return application.snapshot, application.err
}
func (snapshotApplication) Create(context.Context, lobby.Principal, lobby.CreateRequest) (lobby.Snapshot, error) {
	return lobby.Snapshot{}, errors.New("unexpected Create")
}
func (snapshotApplication) Join(context.Context, lobby.Principal, lobby.JoinRequest) (lobby.Snapshot, error) {
	return lobby.Snapshot{}, errors.New("unexpected Join")
}
func (snapshotApplication) SelectTeam(context.Context, lobby.Principal, lobby.Scope, lobby.SelectTeamRequest) (lobby.Snapshot, error) {
	return lobby.Snapshot{}, errors.New("unexpected SelectTeam")
}
func (snapshotApplication) SetReady(context.Context, lobby.Principal, lobby.Scope, lobby.ReadyRequest) (lobby.Snapshot, error) {
	return lobby.Snapshot{}, errors.New("unexpected SetReady")
}
func (snapshotApplication) Start(context.Context, lobby.Principal, lobby.Scope) (lobby.Snapshot, error) {
	return lobby.Snapshot{}, errors.New("unexpected Start")
}
func (snapshotApplication) Leave(context.Context, lobby.Principal, lobby.Scope) error {
	return errors.New("unexpected Leave")
}
func (snapshotApplication) Heartbeat(context.Context, lobby.Principal, lobby.Scope, lobby.HeartbeatRequest) (lobby.HeartbeatResult, error) {
	return lobby.HeartbeatResult{}, errors.New("unexpected Heartbeat")
}
func (snapshotApplication) ReleaseLease(context.Context, lobby.Principal, lobby.Scope, lobby.ReleaseLeaseRequest) error {
	return errors.New("unexpected ReleaseLease")
}

func TestBindStartedLobbyCreatesStableTeamMap(t *testing.T) {
	db := testutils.InitTestDB(t)
	principal, scope, snapshot := startedSnapshot()
	service := newAdapter(t, db, snapshot)
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
	var records []lobbyadapter.TeamBindingRecord
	if err := db.Where(&lobbyadapter.TeamBindingRecord{GameID: result.GameID}).Order("team_order").Find(&records).Error; err != nil {
		t.Fatal(err)
	}
	if len(records) != 2 || records[0].TeamColor == 0 || records[1].TeamColor == 0 {
		t.Fatalf("persisted mappings = %#v", records)
	}
}

func TestBindLobbyIsIdempotent(t *testing.T) {
	db := testutils.InitTestDB(t)
	principal, scope, snapshot := startedSnapshot()
	service := newAdapter(t, db, snapshot)
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
	service := newAdapter(t, db, snapshot)
	start := make(chan struct{})
	results := make(chan lobbyadapter.BindingResult, 2)
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
	service := newAdapter(t, db, snapshot)
	if _, err := service.Bind(context.Background(), nonHost, scope); !errors.Is(err, lobby.ErrNotHost) {
		t.Fatalf("nonhost err = %v", err)
	}
	assertBindingCounts(t, db, scope.LobbyID, 0, 0)
	waiting := snapshot
	waiting.Lobby.Phase = lobby.PhaseWaiting
	if _, err := newAdapter(t, db, waiting).Bind(context.Background(), principal, scope); !errors.Is(err, lobby.ErrPhaseConflict) {
		t.Fatalf("waiting err = %v", err)
	}
	otherScope := scope
	otherScope.GameKey = "quiz-demo"
	if _, err := service.Bind(context.Background(), principal, otherScope); !errors.Is(err, lobby.ErrNotFound) {
		t.Fatalf("other game err = %v", err)
	}
	assertBindingCounts(t, db, scope.LobbyID, 0, 0)
}

func TestBindDoesNotCreateGeneration(t *testing.T) {
	db := testutils.InitTestDB(t)
	principal, scope, snapshot := startedSnapshot()
	if _, err := newAdapter(t, db, snapshot).Bind(context.Background(), principal, scope); err != nil {
		t.Fatal(err)
	}
	var generations int64
	if err := db.Model(&models.Generation{}).Count(&generations).Error; err != nil {
		t.Fatal(err)
	}
	if generations != 0 {
		t.Fatalf("binding created %d generations", generations)
	}
}

func startedSnapshot() (lobby.Principal, lobby.Scope, lobby.Snapshot) {
	playerID, sessionID, lobbyID := uuid.NewString(), uuid.NewString(), uuid.NewString()
	host := playerID
	teamOne, teamTwo := uuid.NewString(), uuid.NewString()
	scope := lobby.Scope{GameKey: "island-battles", LobbyID: lobbyID}
	snapshot := lobby.Snapshot{Lobby: lobby.Lobby{ID: lobbyID, GameKey: scope.GameKey, Phase: lobby.PhaseStarted, HostPlayerID: &host}, Teams: []lobby.Team{{ID: teamOne, LobbyID: lobbyID, Name: "Blue", Capacity: 4, Order: 1}, {ID: teamTwo, LobbyID: lobbyID, Name: "Red", Capacity: 4, Order: 2}}, StartRoster: lobby.StartRoster{{PlayerID: playerID, TeamID: teamOne, MemberID: uuid.NewString(), TeamOrder: 1}, {PlayerID: uuid.NewString(), TeamID: teamOne, MemberID: uuid.NewString(), TeamOrder: 1}}}
	return lobby.Principal{PlayerID: playerID, SessionID: sessionID, DisplayName: "Host"}, scope, snapshot
}

func newAdapter(t *testing.T, db *gorm.DB, snapshot lobby.Snapshot) *lobbyadapter.Service {
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
	if err := db.Model(&lobbyadapter.TeamBindingRecord{}).Count(&bindingCount).Error; err != nil {
		t.Fatal(err)
	}
	if gameCount != games || bindingCount != bindings {
		t.Fatalf("counts games=%d bindings=%d, want games=%d bindings=%d", gameCount, bindingCount, games, bindings)
	}
}
