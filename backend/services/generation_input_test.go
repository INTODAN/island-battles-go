package services_test

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"islandBattles/backend/dal"
	"islandBattles/backend/internal/testutils"
	"islandBattles/backend/models"
	"islandBattles/backend/services"
	"islandBattles/lobby/domain"
)

type generationSnapshots struct {
	snapshot domain.Snapshot
	err      error
}

func (reader generationSnapshots) Snapshot(context.Context, domain.Principal, domain.Scope) (domain.Snapshot, error) {
	return reader.snapshot, reader.err
}

type turnFixture struct {
	database *gorm.DB
	service  *services.GenerationService
	snapshot domain.Snapshot
	players  []domain.Principal
	state    models.GenerationState
}

func newTurnFixture(t *testing.T, teammates bool) turnFixture {
	t.Helper()
	database := testutils.InitTestDB(t)
	lobbyID := uuid.NewString()
	teamOne, teamTwo := uuid.NewString(), uuid.NewString()
	players := []domain.Principal{{PlayerID: uuid.NewString()}, {PlayerID: uuid.NewString()}}
	snapshot := domain.Snapshot{Lobby: domain.Lobby{ID: lobbyID, GameKey: "island-battles", Phase: domain.PhaseStarted}, Teams: []domain.Team{{ID: teamOne, Order: 1}, {ID: teamTwo, Order: 2}}, StartRoster: domain.StartRoster{{PlayerID: players[0].PlayerID, TeamID: teamOne}, {PlayerID: players[1].PlayerID, TeamID: teamTwo}}}
	if teammates {
		players = append(players, domain.Principal{PlayerID: uuid.NewString()})
		snapshot.StartRoster = append(snapshot.StartRoster, domain.StartRosterEntry{PlayerID: players[2].PlayerID, TeamID: teamOne})
	}
	if _, err := services.RunLobbyGame(database, snapshot); err != nil {
		t.Fatal(err)
	}
	service := &services.GenerationService{Database: database, Lobbies: generationSnapshots{snapshot: snapshot}}
	state, err := service.Current(context.Background(), players[0], lobbyID)
	if err != nil {
		t.Fatal(err)
	}
	return turnFixture{database, service, snapshot, players, state}
}
func batch(state models.GenerationState, complete bool, commands ...models.PublicGenerationCommand) models.RegisterGenerationCommands {
	if commands == nil {
		commands = []models.PublicGenerationCommand{}
	}
	return models.RegisterGenerationCommands{GameID: state.GameID, GenerationID: state.GenerationID, RequestID: uuid.NewString(), Complete: &complete, Commands: commands}
}
func send(origin, destination, amount uint) models.PublicGenerationCommand {
	raw, _ := json.Marshal(map[string]uint{"origin_logical_castle_id": origin, "destination_logical_castle_id": destination, "soldier_amount": amount})
	return models.PublicGenerationCommand{CommandType: models.SendSoldierGroup, Parameters: raw}
}
func assertTurnStatus(t *testing.T, err error, status int) {
	t.Helper()
	var rejection *services.GenerationError
	if !errors.As(err, &rejection) || rejection.Status != status {
		t.Fatalf("error=%v, want status %d", err, status)
	}
}
func assertTurnCounts(t *testing.T, database *gorm.DB, generations, inputs, requests, notifications int64) {
	t.Helper()
	for _, check := range []struct {
		model    any
		expected int64
	}{{&models.Generation{}, generations}, {&models.GenerationInput{}, inputs}, {&models.GenerationRequest{}, requests}, {&models.GenerationNotification{}, notifications}} {
		var count int64
		if err := database.Model(check.model).Count(&count).Error; err != nil {
			t.Fatal(err)
		}
		if count != check.expected {
			t.Fatalf("%T count=%d want=%d", check.model, count, check.expected)
		}
	}
}

func TestGenerationTurnsAtomicReservationsRetriesAndRestart(t *testing.T) {
	fixture := newTurnFixture(t, true)
	ctx := context.Background()
	first := batch(fixture.state, false, send(0, 1, 20))
	accepted, err := fixture.service.Register(ctx, fixture.players[0], first)
	if err != nil {
		t.Fatal(err)
	}
	if accepted.Submitted || accepted.GenerationID != fixture.state.GenerationID {
		t.Fatal("incremental request sealed or advanced")
	}
	// A reconstructed service must reserve persisted teammate commands.
	fixture.service = &services.GenerationService{Database: fixture.database, Lobbies: generationSnapshots{snapshot: fixture.snapshot}}
	if _, err := fixture.service.Register(ctx, fixture.players[2], batch(fixture.state, true, send(0, 1, 11))); err == nil {
		t.Fatal("accepted teammate overbooking")
	} else {
		assertTurnStatus(t, err, 409)
	}
	invalid := batch(fixture.state, true, send(0, 1, 5), send(0, 1, 6))
	_, err = fixture.service.Register(ctx, fixture.players[2], invalid)
	assertTurnStatus(t, err, 409)
	assertTurnCounts(t, fixture.database, 1, 1, 1, 3)
	if _, err = fixture.service.Register(ctx, fixture.players[0], first); err != nil {
		t.Fatal(err)
	}
	changed := first
	changed.Commands = []models.PublicGenerationCommand{send(0, 1, 19)}
	_, err = fixture.service.Register(ctx, fixture.players[0], changed)
	assertTurnStatus(t, err, 409)
	if _, err = fixture.service.Register(ctx, fixture.players[2], batch(fixture.state, true, send(0, 1, 10))); err != nil {
		t.Fatal(err)
	}
	seal := batch(fixture.state, true)
	sealed, err := fixture.service.Register(ctx, fixture.players[0], seal)
	if err != nil || !sealed.Submitted {
		t.Fatalf("seal=%+v err=%v", sealed, err)
	}
	_, err = fixture.service.Register(ctx, fixture.players[0], batch(fixture.state, false))
	assertTurnStatus(t, err, 409)
	// Frozen player two is still required even with no active snapshot members.
	final := batch(fixture.state, true)
	next, err := fixture.service.Register(ctx, fixture.players[1], final)
	if err != nil {
		t.Fatal(err)
	}
	if next.GenerationID == fixture.state.GenerationID || next.Submitted {
		t.Fatalf("next=%+v", next)
	}
	if next.Generation.Castles[0].Soldiers != 1 || len(next.Generation.SoldierGroups) != 2 {
		t.Fatalf("commands not applied: %+v", next.Generation)
	}
	retry, err := fixture.service.Register(ctx, fixture.players[0], first)
	if err != nil || retry.GenerationID != next.GenerationID {
		t.Fatalf("retry=%+v err=%v", retry, err)
	}
	_, err = fixture.service.Register(ctx, fixture.players[0], batch(fixture.state, true))
	assertTurnStatus(t, err, 409)
	assertTurnCounts(t, fixture.database, 2, 3, 4, 6)
	var notifications []models.GenerationNotification
	if err := fixture.database.Find(&notifications).Error; err != nil {
		t.Fatal(err)
	}
	for _, notification := range notifications {
		var event struct {
			Type  string                 `json:"type"`
			State models.GenerationState `json:"state"`
		}
		if err := json.Unmarshal(notification.Payload, &event); err != nil {
			t.Fatal(err)
		}
		if event.Type != "game.next_generation" || event.State.PlayerID != notification.PlayerID || event.State.GenerationID != notification.GenerationID || event.State.Submitted || len(event.State.Generation.Castles) != 2 {
			t.Fatalf("bad event: %s", notification.Payload)
		}
	}
}

func TestGenerationCommandValidationAndAuthorization(t *testing.T) {
	fixture := newTurnFixture(t, false)
	ctx := context.Background()
	cases := []struct {
		name    string
		command models.PublicGenerationCommand
		status  int
	}{
		{"server command", models.PublicGenerationCommand{CommandType: models.MoveSoldierGroups, Parameters: json.RawMessage(`{}`)}, 403},
		{"unknown command", models.PublicGenerationCommand{CommandType: "oops", Parameters: json.RawMessage(`{}`)}, 403},
		{"ownership", send(1, 0, 1), 403}, {"missing castle", send(0, 123, 1), 422}, {"self target", send(0, 0, 1), 422}, {"zero amount", send(0, 1, 0), 422},
		{"missing zero ID", models.PublicGenerationCommand{CommandType: models.SendSoldierGroup, Parameters: json.RawMessage(`{"destination_logical_castle_id":1,"soldier_amount":1}`)}, 422},
		{"fraction", models.PublicGenerationCommand{CommandType: models.SendSoldierGroup, Parameters: json.RawMessage(`{"origin_logical_castle_id":0,"destination_logical_castle_id":1,"soldier_amount":1.5}`)}, 422},
		{"negative", models.PublicGenerationCommand{CommandType: models.SendSoldierGroup, Parameters: json.RawMessage(`{"origin_logical_castle_id":0,"destination_logical_castle_id":1,"soldier_amount":-1}`)}, 422},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			_, err := fixture.service.Register(ctx, fixture.players[0], batch(fixture.state, true, test.command))
			assertTurnStatus(t, err, test.status)
		})
	}
	outsider := domain.Principal{PlayerID: uuid.NewString()}
	_, err := fixture.service.Register(ctx, outsider, batch(fixture.state, true))
	assertTurnStatus(t, err, 404)
	_, err = fixture.service.Current(ctx, outsider, fixture.state.LobbyID)
	assertTurnStatus(t, err, 404)
	// Even a forged snapshot cannot add an outsider to the persisted roster.
	forged := fixture.snapshot
	forged.StartRoster = append(forged.StartRoster, domain.StartRosterEntry{PlayerID: outsider.PlayerID, TeamID: forged.Teams[0].ID})
	forgedService := services.GenerationService{Database: fixture.database, Lobbies: generationSnapshots{snapshot: forged}}
	_, err = forgedService.Register(ctx, outsider, batch(fixture.state, true))
	assertTurnStatus(t, err, 404)
	unauthorized := services.GenerationService{Database: fixture.database, Lobbies: generationSnapshots{err: domain.ErrNotFound}}
	_, err = unauthorized.Register(ctx, fixture.players[0], batch(fixture.state, true))
	assertTurnStatus(t, err, 404)
	stale := batch(fixture.state, true)
	stale.GenerationID += 100
	_, err = fixture.service.Register(ctx, fixture.players[0], stale)
	assertTurnStatus(t, err, 409)
	detachedID, err := dal.CreateGame(fixture.database)
	if err != nil {
		t.Fatal(err)
	}
	foreign := batch(fixture.state, true)
	foreign.GameID = detachedID
	_, err = fixture.service.Register(ctx, fixture.players[0], foreign)
	assertTurnStatus(t, err, 404)
	assertTurnCounts(t, fixture.database, 1, 0, 0, 2)
}

func TestGenerationConcurrentCompletionsAdvanceOnce(t *testing.T) {
	fixture := newTurnFixture(t, false)
	start := make(chan struct{})
	failures := make(chan error, 4)
	var group sync.WaitGroup
	// Both identical retries and competing final players serialize on the game.
	for _, player := range fixture.players {
		request := batch(fixture.state, true)
		for range 2 {
			group.Add(1)
			go func(principal domain.Principal, request models.RegisterGenerationCommands) {
				defer group.Done()
				<-start
				_, err := fixture.service.Register(context.Background(), principal, request)
				failures <- err
			}(player, request)
		}
	}
	close(start)
	group.Wait()
	close(failures)
	for err := range failures {
		if err != nil {
			t.Fatal(err)
		}
	}
	assertTurnCounts(t, fixture.database, 2, 2, 2, 4)
}

func TestGenerationMigrationFailureRollsBackFinalRequest(t *testing.T) {
	fixture := newTurnFixture(t, false)
	ctx := context.Background()
	if _, err := fixture.service.Register(ctx, fixture.players[0], batch(fixture.state, true, send(0, 1, 5))); err != nil {
		t.Fatal(err)
	}
	failure := errors.New("injected engine failure")
	callback := "test:fail-turn-command"
	if err := fixture.database.Callback().Create().Before("gorm:create").Register(callback, func(transaction *gorm.DB) {
		if transaction.Statement.Table == "soldier_groups" {
			transaction.AddError(failure)
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = fixture.database.Callback().Create().Remove(callback) })
	final := batch(fixture.state, true, send(1, 0, 4))
	_, err := fixture.service.Register(ctx, fixture.players[1], final)
	if !errors.Is(err, failure) {
		t.Fatalf("err=%v", err)
	}
	assertTurnCounts(t, fixture.database, 1, 1, 1, 2)
	current, err := fixture.service.Current(ctx, fixture.players[1], fixture.state.LobbyID)
	if err != nil || current.Submitted {
		t.Fatalf("current=%+v err=%v", current, err)
	}
	if err := fixture.database.Callback().Create().Remove(callback); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.service.Register(ctx, fixture.players[1], final); err != nil {
		t.Fatal(err)
	}
	assertTurnCounts(t, fixture.database, 2, 2, 2, 4)
}

func TestGenerationNotificationFailureRollsBackMigration(t *testing.T) {
	fixture := newTurnFixture(t, false)
	ctx := context.Background()
	if _, err := fixture.service.Register(ctx, fixture.players[0], batch(fixture.state, true)); err != nil {
		t.Fatal(err)
	}
	failure := errors.New("injected notification failure")
	callback := "test:fail-turn-notification"
	if err := fixture.database.Callback().Create().Before("gorm:create").Register(callback, func(transaction *gorm.DB) {
		if transaction.Statement.Table == "generation_notifications" {
			transaction.AddError(failure)
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = fixture.database.Callback().Create().Remove(callback) })
	final := batch(fixture.state, true)
	_, err := fixture.service.Register(ctx, fixture.players[1], final)
	if !errors.Is(err, failure) {
		t.Fatalf("err=%v", err)
	}
	assertTurnCounts(t, fixture.database, 1, 1, 1, 2)
	var maps int64
	if err := fixture.database.Model(&models.Map{}).Count(&maps).Error; err != nil {
		t.Fatal(err)
	}
	if maps != 1 {
		t.Fatalf("migration left %d maps", maps)
	}
	if err := fixture.database.Callback().Create().Remove(callback); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.service.Register(ctx, fixture.players[1], final); err != nil {
		t.Fatal(err)
	}
	assertTurnCounts(t, fixture.database, 2, 2, 2, 4)
}
