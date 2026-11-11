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

type turnFixture struct {
	database  *gorm.DB
	publisher *testutils.RecordedPublisher
	service   *services.GenerationService
	players   []domain.Principal
	state     models.GenerationState
}

func newTurnFixture(t *testing.T) turnFixture {
	t.Helper()
	database := testutils.InitTestDB(t)
	lobbyID := uuid.NewString()
	teamOne, teamTwo := uuid.NewString(), uuid.NewString()
	players := []domain.Principal{{PlayerID: uuid.NewString()}, {PlayerID: uuid.NewString()}}
	snapshot := domain.Snapshot{Lobby: domain.Lobby{ID: lobbyID, GameKey: "island-battles", Phase: domain.PhaseStarted}, Teams: []domain.Team{{ID: teamOne, Order: 1, Name: "Blue"}, {ID: teamTwo, Order: 2, Name: "Red"}}, StartRoster: domain.StartRoster{{PlayerID: players[0].PlayerID, TeamID: teamOne}, {PlayerID: players[1].PlayerID, TeamID: teamTwo}}}
	publisher := &testutils.RecordedPublisher{}
	if _, err := services.RunLobbyGame(context.Background(), database, publisher, snapshot); err != nil {
		t.Fatal(err)
	}
	service := &services.GenerationService{Database: database, Publisher: publisher}
	state, err := service.Current(context.Background(), players[0], lobbyID)
	if err != nil {
		t.Fatal(err)
	}
	return turnFixture{database, publisher, service, players, state}
}
func batch(state models.GenerationState, complete bool, commands ...models.PublicGenerationCommand) models.RegisterGenerationCommands {
	if commands == nil {
		commands = []models.PublicGenerationCommand{}
	}
	return models.RegisterGenerationCommands{GameID: state.GameID, GenerationID: state.GenerationID, RequestID: uuid.NewString(), Complete: &complete, Commands: commands}
}
func send(origin, destination, amount uint) models.PublicGenerationCommand {
	raw, _ := json.Marshal(map[string]uint{"origin_castle_id": origin, "destination_castle_id": destination, "soldier_amount": amount})
	return models.PublicGenerationCommand{CommandType: models.SendSoldierGroup, Parameters: raw}
}
func assertTurnStatus(t *testing.T, err error, status int) {
	t.Helper()
	var rejection *services.GenerationError
	if !errors.As(err, &rejection) || rejection.Status != status {
		t.Fatalf("error=%v, want status %d", err, status)
	}
}

// assertTurnCounts checks stored turn state and the generation events pushed so
// far, one per roster player for each generation.
func assertTurnCounts(t *testing.T, fixture turnFixture, generations, inputs, requests int64, pushed int) {
	t.Helper()
	for _, check := range []struct {
		model    any
		expected int64
	}{{&models.Generation{}, generations}, {&models.GenerationInput{}, inputs}, {&models.GenerationRequest{}, requests}} {
		var count int64
		if err := fixture.database.Model(check.model).Count(&count).Error; err != nil {
			t.Fatal(err)
		}
		if count != check.expected {
			t.Fatalf("%T count=%d want=%d", check.model, count, check.expected)
		}
	}
	if published := len(fixture.publisher.Publications()); published != pushed {
		t.Fatalf("pushed %d generation events, want %d", published, pushed)
	}
}

func TestGenerationTurnsAtomicReservationsRetriesAndRestart(t *testing.T) {
	fixture := newTurnFixture(t)
	ctx := context.Background()
	first := batch(fixture.state, false, send(0, 1, 20))
	accepted, err := fixture.service.Register(ctx, fixture.players[0], first)
	if err != nil {
		t.Fatal(err)
	}
	if accepted.Submitted || accepted.GenerationID != fixture.state.GenerationID {
		t.Fatal("incremental request sealed or advanced")
	}
	// A reconstructed service must reserve the player's persisted earlier batch.
	fixture.service = &services.GenerationService{Database: fixture.database, Publisher: fixture.publisher}
	if _, err := fixture.service.Register(ctx, fixture.players[0], batch(fixture.state, false, send(0, 1, 11))); err == nil {
		t.Fatal("accepted overbooking across batches")
	} else {
		assertTurnStatus(t, err, 409)
	}
	invalid := batch(fixture.state, false, send(0, 1, 5), send(0, 1, 6))
	_, err = fixture.service.Register(ctx, fixture.players[0], invalid)
	assertTurnStatus(t, err, 409)
	assertTurnCounts(t, fixture, 1, 1, 1, 2)
	if _, err = fixture.service.Register(ctx, fixture.players[0], first); err != nil {
		t.Fatal(err)
	}
	changed := first
	changed.Commands = []models.PublicGenerationCommand{send(0, 1, 19)}
	_, err = fixture.service.Register(ctx, fixture.players[0], changed)
	assertTurnStatus(t, err, 409)
	if _, err = fixture.service.Register(ctx, fixture.players[0], batch(fixture.state, false, send(0, 1, 10))); err != nil {
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
	assertTurnCounts(t, fixture, 2, 2, 4, 4)
	// Each roster player was pushed the initial generation, then the next one.
	recipients := map[uint]map[string]bool{}
	for _, event := range fixture.publisher.GenerationEvents(t) {
		if event.Type != "game.next_generation" || event.State.Submitted || len(event.State.Generation.Castles) != 2 {
			t.Fatalf("bad event: %+v", event)
		}
		if recipients[event.State.GenerationID] == nil {
			recipients[event.State.GenerationID] = map[string]bool{}
		}
		recipients[event.State.GenerationID][event.State.PlayerID] = true
	}
	for _, generationID := range []uint{fixture.state.GenerationID, next.GenerationID} {
		for _, player := range fixture.players {
			if !recipients[generationID][player.PlayerID] {
				t.Fatalf("generation %d was not pushed to %s: %v", generationID, player.PlayerID, recipients)
			}
		}
	}
}

func TestGenerationCommandValidationAndAuthorization(t *testing.T) {
	fixture := newTurnFixture(t)
	ctx := context.Background()
	cases := []struct {
		name    string
		command models.PublicGenerationCommand
		status  int
	}{
		{"server command", models.PublicGenerationCommand{CommandType: models.MoveSoldierGroups, Parameters: json.RawMessage(`{}`)}, 403},
		{"unknown command", models.PublicGenerationCommand{CommandType: "oops", Parameters: json.RawMessage(`{}`)}, 403},
		{"ownership", send(1, 0, 1), 403}, {"missing castle", send(0, 123, 1), 422}, {"self target", send(0, 0, 1), 422}, {"zero amount", send(0, 1, 0), 422},
		{"missing zero ID", models.PublicGenerationCommand{CommandType: models.SendSoldierGroup, Parameters: json.RawMessage(`{"destination_castle_id":1,"soldier_amount":1}`)}, 422},
		{"fraction", models.PublicGenerationCommand{CommandType: models.SendSoldierGroup, Parameters: json.RawMessage(`{"origin_castle_id":0,"destination_castle_id":1,"soldier_amount":1.5}`)}, 422},
		{"negative", models.PublicGenerationCommand{CommandType: models.SendSoldierGroup, Parameters: json.RawMessage(`{"origin_castle_id":0,"destination_castle_id":1,"soldier_amount":-1}`)}, 422},
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
	_, err = fixture.service.Replay(ctx, outsider, fixture.state.LobbyID)
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
	assertTurnCounts(t, fixture, 1, 0, 0, 2)
}

// A player on another game's roster is still an outsider to this game.
func TestGenerationRejectsAnotherGamesRoster(t *testing.T) {
	fixture := newTurnFixture(t)
	ctx := context.Background()
	otherLobby, teamOne, teamTwo := uuid.NewString(), uuid.NewString(), uuid.NewString()
	elsewhere := domain.Principal{PlayerID: uuid.NewString()}
	other := domain.Snapshot{Lobby: domain.Lobby{ID: otherLobby, GameKey: "island-battles", Phase: domain.PhaseStarted}, Teams: []domain.Team{{ID: teamOne, Order: 1}, {ID: teamTwo, Order: 2}}, StartRoster: domain.StartRoster{{PlayerID: elsewhere.PlayerID, TeamID: teamOne}, {PlayerID: uuid.NewString(), TeamID: teamTwo}}}
	if _, err := services.RunLobbyGame(ctx, fixture.database, fixture.publisher, other); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.service.Current(ctx, elsewhere, otherLobby); err != nil {
		t.Fatalf("own game read: %v", err)
	}
	if replay, err := fixture.service.Replay(ctx, elsewhere, otherLobby); err != nil || len(replay.Frames) == 0 {
		t.Fatalf("own game replay: %v", err)
	}
	_, err := fixture.service.Register(ctx, elsewhere, batch(fixture.state, true))
	assertTurnStatus(t, err, 404)
	_, err = fixture.service.Current(ctx, elsewhere, fixture.state.LobbyID)
	assertTurnStatus(t, err, 404)
	_, err = fixture.service.Replay(ctx, elsewhere, fixture.state.LobbyID)
	assertTurnStatus(t, err, 404)
	// Both games still sit at their first generation with nothing submitted.
	assertTurnCounts(t, fixture, 2, 0, 0, 4)
}

func TestGenerationConcurrentCompletionsAdvanceOnce(t *testing.T) {
	fixture := newTurnFixture(t)
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
	assertTurnCounts(t, fixture, 2, 2, 2, 4)
}

func TestGenerationMigrationFailureRollsBackFinalRequest(t *testing.T) {
	fixture := newTurnFixture(t)
	ctx := context.Background()
	if _, err := fixture.service.Register(ctx, fixture.players[0], batch(fixture.state, true, send(0, 1, 5))); err != nil {
		t.Fatal(err)
	}
	failure := errors.New("injected engine failure")
	callback := "test:fail-turn-command"
	if err := fixture.database.Callback().Create().Before("gorm:create").Register(callback, func(transaction *gorm.DB) {
		if transaction.Statement.Table == "soldier_groups" {
			_ = transaction.AddError(failure)
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
	assertTurnCounts(t, fixture, 1, 1, 1, 2)
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
	assertTurnCounts(t, fixture, 2, 2, 2, 4)
}

func TestGenerationPushFailureKeepsCommittedTurn(t *testing.T) {
	fixture := newTurnFixture(t)
	ctx := context.Background()
	if _, err := fixture.service.Register(ctx, fixture.players[0], batch(fixture.state, true)); err != nil {
		t.Fatal(err)
	}
	// Centrifugo is unreachable: the push fails after its retries, but the turn
	// stands and players read the new generation over HTTP.
	fixture.publisher.Err = errors.New("centrifugo unreachable")
	next, err := fixture.service.Register(ctx, fixture.players[1], batch(fixture.state, true))
	if err != nil || next.GenerationID == fixture.state.GenerationID {
		t.Fatalf("next=%+v err=%v", next, err)
	}
	assertTurnCounts(t, fixture, 2, 2, 2, 4)
	current, err := fixture.service.Current(ctx, fixture.players[0], fixture.state.LobbyID)
	if err != nil || current.GenerationID != next.GenerationID {
		t.Fatalf("current=%+v err=%v", current, err)
	}
}

// The turn that creates the last generation ends the game, and with equal
// castle counts the game is a draw.
func TestGenerationCapEndsTheGame(t *testing.T) {
	fixture := newTurnFixture(t)
	ctx := context.Background()
	initial, err := dal.GetGeneration(fixture.database, fixture.state.GenerationID)
	if err != nil {
		t.Fatal(err)
	}
	// Earlier turns only need to exist, so these stand-ins share the board.
	played := make([]models.Generation, services.MaxGenerations-2)
	for index := range played {
		played[index] = models.Generation{GameID: fixture.state.GameID, MapID: initial.MapID}
	}
	if err := fixture.database.Create(&played).Error; err != nil {
		t.Fatal(err)
	}
	penultimate, err := fixture.service.Current(ctx, fixture.players[0], fixture.state.LobbyID)
	if err != nil {
		t.Fatal(err)
	}
	var last models.GenerationState
	for _, player := range fixture.players {
		if last, err = fixture.service.Register(ctx, player, batch(penultimate, true)); err != nil {
			t.Fatal(err)
		}
	}
	if last.GenerationID == penultimate.GenerationID {
		t.Fatal("the generation before the cap did not advance")
	}
	draw := models.GameOutcomeView{Reason: models.GameOutcomeGenerationCap, Summary: "Draw: equal castle counts at the generation limit"}
	if penultimate.Outcome != nil || last.Outcome == nil || *last.Outcome != draw {
		t.Fatalf("outcome before the cap = %+v, at the cap = %+v", penultimate.Outcome, last.Outcome)
	}
	if ended := fixture.publisher.GameEndedEvents(t); len(ended) != len(fixture.players) || ended[0].State.GenerationID != last.GenerationID {
		t.Fatalf("game.ended events = %+v", ended)
	}
	_, err = fixture.service.Register(ctx, fixture.players[0], batch(last, true))
	assertGameEnded(t, err)
	var generations int64
	if err := fixture.database.Model(&models.Generation{}).Where(&models.Generation{GameID: fixture.state.GameID}).Count(&generations).Error; err != nil {
		t.Fatal(err)
	}
	if generations != services.MaxGenerations {
		t.Fatalf("game has %d generations, want %d", generations, services.MaxGenerations)
	}
	if current, err := fixture.service.Current(ctx, fixture.players[1], fixture.state.LobbyID); err != nil || current.GenerationID != last.GenerationID || current.Submitted {
		t.Fatalf("capped game cannot be read: %+v %v", current, err)
	}
}

func TestStartingARosterWithTwoPlayersOnOneTeamFails(t *testing.T) {
	database := testutils.InitTestDB(t)
	team := uuid.NewString()
	snapshot := domain.Snapshot{
		Lobby:       domain.Lobby{ID: uuid.NewString(), GameKey: "island-battles", Phase: domain.PhaseStarted},
		Teams:       []domain.Team{{ID: team, Order: 1, Name: "Blue"}},
		StartRoster: domain.StartRoster{{PlayerID: uuid.NewString(), TeamID: team}, {PlayerID: uuid.NewString(), TeamID: team}},
	}
	if _, err := services.RunLobbyGame(context.Background(), database, &testutils.RecordedPublisher{}, snapshot); err == nil {
		t.Fatal("started a game with two players on one team")
	}
	for _, model := range []any{&models.Game{}, &models.Generation{}, &models.FrozenGamePlayer{}} {
		var count int64
		if err := database.Model(model).Count(&count).Error; err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Fatalf("%T count=%d, want no game created", model, count)
		}
	}
}
