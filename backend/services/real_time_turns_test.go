package services_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/INTODAN/mpl/domain"
	"islandBattles/backend/dal"
	"islandBattles/backend/internal/testutils"
	"islandBattles/backend/models"
	"islandBattles/backend/services"
	"islandBattles/backend/submittoken"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"gorm.io/gorm"
)

// clockGame is a started game, one player per team, whose service runs on a
// clock the test sets, with fake bot runners for its hosted and uploaded
// bots. players[i] plays bots[i] on team i+1.
type clockGame struct {
	database  *gorm.DB
	publisher *testutils.RecordedPublisher
	runners   *fakeRunners
	tokens    *submittoken.Signer
	service   *services.GenerationService
	clock     *time.Time
	lobbyID   string
	gameID    uint
	players   []domain.Principal
}

// startClockGame starts a game on the circle map with the lobby settings,
// a JSON object or empty, and one player per bot. A zero ServerBot leaves
// the player to their localhost bot.
func startClockGame(t *testing.T, settings string, bots ...services.ServerBot) clockGame {
	t.Helper()
	database := testutils.InitTestDB(t)
	tokens, err := submittoken.New([]byte("real-time-test-secret"))
	if err != nil {
		t.Fatal(err)
	}
	clock := time.Now()
	game := clockGame{database: database, publisher: &testutils.RecordedPublisher{}, tokens: tokens, clock: &clock, lobbyID: uuid.NewString()}
	game.runners = &fakeRunners{database: database, tokens: tokens, failing: map[string]bool{}}
	sender := &services.TurnSender{Runners: game.runners, Packages: fakeLinks{}, Tokens: tokens, SubmitURL: serverBotSubmitURL}
	game.service = &services.GenerationService{Database: database, Publisher: game.publisher, Bots: sender, Now: func() time.Time { return *game.clock }}
	snapshot := domain.Snapshot{Lobby: domain.Lobby{ID: game.lobbyID, GameKey: "island-battles", Phase: domain.PhaseStarted, GameSettings: testutils.CircleMapSettings(len(bots), settings)}}
	chosen := map[string]services.ServerBot{}
	for index, bot := range bots {
		team := domain.Team{ID: uuid.NewString(), Order: index + 1, Name: deadlineTeamNames[index]}
		snapshot.Teams = append(snapshot.Teams, team)
		player := domain.Principal{PlayerID: uuid.NewString()}
		game.players = append(game.players, player)
		snapshot.StartRoster = append(snapshot.StartRoster, domain.StartRosterEntry{PlayerID: player.PlayerID, TeamID: team.ID})
		snapshot.Members = append(snapshot.Members, domain.Member{PlayerID: player.PlayerID, DisplayName: fmt.Sprintf("Player %d", index+1)})
		if bot != (services.ServerBot{}) {
			chosen[player.PlayerID] = bot
		}
	}
	binding, err := services.RunLobbyGameWithServerBots(context.Background(), database, game.publisher, sender, snapshot, chosen)
	if err != nil {
		t.Fatal(err)
	}
	game.gameID = binding.GameID
	return game
}

func (game clockGame) stored(t *testing.T) *models.Game {
	t.Helper()
	stored, err := dal.FindGame(game.database, game.gameID)
	if err != nil {
		t.Fatal(err)
	}
	return stored
}

func (game clockGame) open(t *testing.T) *models.Generation {
	t.Helper()
	generation, err := dal.GetLatestGenerationForGame(game.database, game.gameID)
	if err != nil {
		t.Fatal(err)
	}
	return generation
}

// generationCount is how many generations the game has.
func (game clockGame) generationCount(t *testing.T) int {
	t.Helper()
	generations, err := dal.LoadGameGenerations(game.database, game.gameID)
	if err != nil {
		t.Fatal(err)
	}
	return len(generations)
}

// turnEnd is when the open turn ends: its stored deadline, or, for the first
// generation, its creation time plus the game's pace.
func (game clockGame) turnEnd(t *testing.T) time.Time {
	t.Helper()
	pace := game.stored(t).TurnPaceMilliseconds
	if pace == nil {
		t.Fatal("the game isn't real-time")
	}
	open := game.open(t)
	if open.TurnDeadlineAt != nil {
		return *open.TurnDeadlineAt
	}
	return open.CreatedAt.Add(time.Duration(*pace) * time.Millisecond)
}

func (game clockGame) current(t *testing.T, player domain.Principal) models.GenerationState {
	t.Helper()
	state, err := game.service.Current(context.Background(), player, game.lobbyID)
	if err != nil {
		t.Fatal(err)
	}
	return state
}

func (game clockGame) applyDeadline(t *testing.T, player domain.Principal) models.GenerationState {
	t.Helper()
	state, err := game.service.ApplyDeadline(context.Background(), player, game.lobbyID)
	if err != nil {
		t.Fatal(err)
	}
	return state
}

func (game clockGame) register(t *testing.T, player domain.Principal, request models.RegisterGenerationCommands) models.GenerationState {
	t.Helper()
	state, err := game.service.Register(context.Background(), player, request)
	if err != nil {
		t.Fatal(err)
	}
	return state
}

// eventsOf counts the game.next_generation events pushed for a generation.
func (game clockGame) eventsOf(t *testing.T, generationID uint) int {
	t.Helper()
	count := 0
	for _, event := range game.publisher.GenerationEvents(t) {
		if event.State.GenerationID == generationID {
			count++
		}
	}
	return count
}

// attack is a command that sends soldiers from the player's castle to the
// next team's castle.
func attack(t *testing.T, state models.GenerationState, soldiers uint) models.PublicGenerationCommand {
	t.Helper()
	var own, other *models.CastleView
	for index, castle := range state.Generation.Castles {
		if castle.TeamColor == state.TeamColor {
			own = &state.Generation.Castles[index]
		} else if other == nil {
			other = &state.Generation.Castles[index]
		}
	}
	if own == nil || other == nil {
		t.Fatalf("no castles to attack with in %+v", state.Generation.Castles)
	}
	return send(own.ID, other.ID, soldiers)
}

// sentTo is the player an event's submit token stands for.
func sentTo(t *testing.T, tokens *submittoken.Signer, event models.BotRunnerEvent) string {
	t.Helper()
	claims, err := tokens.Verify(event.Delivery.Submit.Token, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	return claims.PlayerID
}

// hasGroup reports whether a team has a group of soldiers on the field.
func hasGroup(state models.GenerationState, team, soldiers uint) bool {
	for _, group := range state.Generation.SoldierGroups {
		if group.TeamColor == team && group.Soldiers == soldiers {
			return true
		}
	}
	return false
}

func TestAGameWithAManualPlayerStoresThePace(t *testing.T) {
	game := startClockGame(t, `{"turn_pace_milliseconds":500}`, services.ServerBot{Manual: true}, services.ServerBot{Level: 2})
	manual := game.players[0]
	state := game.current(t, manual)
	if state.TurnPaceMilliseconds == nil || *state.TurnPaceMilliseconds != 500 {
		t.Fatalf("turn_pace_milliseconds = %v", state.TurnPaceMilliseconds)
	}
	// The turn ends one pace after its generation was created, everywhere
	// turn_deadline_at appears.
	if want := game.open(t).CreatedAt.Add(500 * time.Millisecond); state.TurnDeadlineAt == nil || !state.TurnDeadlineAt.Equal(want) {
		t.Fatalf("turn_deadline_at = %v, want %v", state.TurnDeadlineAt, want)
	}
	watch, err := game.service.Watch(context.Background(), manual, game.lobbyID)
	if err != nil {
		t.Fatal(err)
	}
	if watch.TurnPaceMilliseconds == nil || *watch.TurnPaceMilliseconds != 500 || watch.TurnDeadlineAt == nil || !watch.TurnDeadlineAt.Equal(*state.TurnDeadlineAt) {
		t.Fatalf("watch pace %v, deadline %v", watch.TurnPaceMilliseconds, watch.TurnDeadlineAt)
	}
	// The turn timeout is still copied, for #347, but no turn times out.
	if timeout := game.stored(t).TurnTimeoutSeconds; timeout == nil || *timeout != models.DefaultTurnTimeoutSeconds {
		t.Fatalf("turn_timeout_seconds = %v", timeout)
	}

	// Without the setting, the pace is a second.
	defaults := startClockGame(t, ``, services.ServerBot{Manual: true}, services.ServerBot{Level: 2})
	if pace := defaults.current(t, defaults.players[1]).TurnPaceMilliseconds; pace == nil || *pace != models.DefaultTurnPaceMilliseconds {
		t.Fatalf("default turn_pace_milliseconds = %v", pace)
	}
}

func TestADeadlineCallPlaysARealTimeTurnWithTheCommandsThatArrived(t *testing.T) {
	game := startClockGame(t, ``, services.ServerBot{Manual: true}, services.ServerBot{Level: 2})
	manual := game.players[0]
	first := game.current(t, manual)
	// The hand player's client sends each order as a batch that doesn't
	// complete the turn.
	game.register(t, manual, batch(first, false, attack(t, first, 5)))
	end := game.turnEnd(t)

	*game.clock = end.Add(-time.Millisecond)
	pushed := len(game.publisher.Publications())
	if state := game.applyDeadline(t, manual); state.GenerationID != first.GenerationID || state.Submitted {
		t.Fatalf("a deadline call before the pace = %+v", state)
	}
	if len(game.publisher.Publications()) != pushed {
		t.Fatal("a deadline call before the pace pushed an event")
	}

	*game.clock = end
	next := game.applyDeadline(t, manual)
	if next.GenerationID == first.GenerationID || next.TurnNumber != 2 || next.Outcome != nil || next.Submitted {
		t.Fatalf("the deadline call after the pace = %+v", next)
	}
	if !hasGroup(next, first.TeamColor, 5) {
		t.Fatalf("the manual player's command wasn't played: %+v", next.Generation.SoldierGroups)
	}
	// The manual player completed nothing, so no memory was saved.
	if next.Memory != nil {
		t.Fatalf("memory = %s", next.Memory)
	}
	// Every roster player gets the new generation once; the Go bot has
	// already played it.
	if events := game.eventsOf(t, next.GenerationID); events != 2 {
		t.Fatalf("pushed the new generation %d times, want once per player", events)
	}
	if bot := game.current(t, game.players[1]); !bot.Submitted || bot.GenerationID != next.GenerationID {
		t.Fatalf("the Go bot's state = %+v", bot)
	}

	pushed = len(game.publisher.Publications())
	if again := game.applyDeadline(t, manual); again.GenerationID != next.GenerationID {
		t.Fatalf("a second deadline call played generation %d", again.GenerationID)
	}
	if len(game.publisher.Publications()) != pushed {
		t.Fatal("a second deadline call pushed an event")
	}
}

func TestAManualPlayerWhoSendsNothingStaysInTheGame(t *testing.T) {
	game := startClockGame(t, `{"turn_timeout_seconds":5}`, services.ServerBot{Manual: true}, services.ServerBot{Level: 2})
	manual := game.players[0]
	for turn := range 10 {
		// At each turn's end the client's deadline call plays the turn. It
		// sends no command, and isn't silent: it calls.
		*game.clock = game.turnEnd(t)
		state := game.applyDeadline(t, manual)
		if state.TurnNumber != turn+2 || state.Outcome != nil {
			t.Fatalf("turn %d: state %+v", turn, state)
		}
	}
	state := game.current(t, manual)
	if state.Outcome != nil || game.stored(t).Outcome != nil || len(game.publisher.GameEndedEvents(t)) != 0 {
		t.Fatalf("the game ended: %+v", state.Outcome)
	}
	owned := false
	for _, castle := range state.Generation.Castles {
		owned = owned || castle.TeamColor == state.TeamColor
	}
	if !owned {
		t.Fatal("the manual player's team lost its castle")
	}
}

func TestTwoManualPlayersShareATurnAndRacingDeadlineCallsPlayItOnce(t *testing.T) {
	game := startClockGame(t, ``, services.ServerBot{Manual: true}, services.ServerBot{Manual: true})
	red, blue := game.players[0], game.players[1]
	redState, blueState := game.current(t, red), game.current(t, blue)
	game.register(t, red, batch(redState, false, attack(t, redState, 4)))
	game.register(t, blue, batch(blueState, false, attack(t, blueState, 6)))
	generations := game.generationCount(t)
	*game.clock = game.turnEnd(t)

	states := make([]models.GenerationState, 2)
	errs := make([]error, 2)
	var wait sync.WaitGroup
	start := make(chan struct{})
	for index, player := range []domain.Principal{red, blue} {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			states[index], errs[index] = game.service.ApplyDeadline(context.Background(), player, game.lobbyID)
		}()
	}
	close(start)
	wait.Wait()
	for _, err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if count := game.generationCount(t); count != generations+1 {
		t.Fatalf("racing deadline calls created %d generations, want 1", count-generations)
	}
	next := game.open(t)
	for index, state := range states {
		if state.GenerationID != next.ID {
			t.Fatalf("caller %d got generation %d, want %d", index, state.GenerationID, next.ID)
		}
	}
	if events := game.eventsOf(t, next.ID); events != 2 {
		t.Fatalf("pushed the new generation %d times, want once per player", events)
	}
	if !hasGroup(states[0], redState.TeamColor, 4) || !hasGroup(states[0], blueState.TeamColor, 6) {
		t.Fatalf("both players' commands should land in the same turn: %+v", states[0].Generation.SoldierGroups)
	}
}

// A batch that arrives after its turn played joins the open turn while it
// names one of the two turns before it, and is checked against the open
// turn's board; an older one is stale.
func TestALateBatchJoinsTheOpenTurnForTwoTurns(t *testing.T) {
	game := startClockGame(t, `{"turn_pace_milliseconds":2000}`, services.ServerBot{Manual: true}, services.ServerBot{Level: 2})
	manual := game.players[0]
	first := game.current(t, manual)
	game.register(t, manual, batch(first, false, attack(t, first, 3)))
	*game.clock = game.turnEnd(t).Add(time.Millisecond)

	// The late batch's request plays the turn it missed, with the batch
	// that arrived in time, and then joins the new open turn.
	second := game.register(t, manual, batch(first, false, attack(t, first, 2)))
	if second.TurnNumber != 2 || second.Submitted || !hasGroup(second, first.TeamColor, 3) || hasGroup(second, first.TeamColor, 2) {
		t.Fatalf("after the late batch: turn %d, groups %+v", second.TurnNumber, second.Generation.SoldierGroups)
	}
	if events := game.eventsOf(t, second.GenerationID); events != 2 {
		t.Fatalf("pushed the new generation %d times", events)
	}
	third := game.toTurnEnd(t, manual)
	if third.TurnNumber != 3 || !hasGroup(third, first.TeamColor, 2) {
		t.Fatalf("the late batch wasn't played in the open turn: turn %d, groups %+v", third.TurnNumber, third.Generation.SoldierGroups)
	}

	// Two turns later the first turn's batch still joins, checked against
	// the open turn's board: more soldiers than the castle has now is
	// refused for that.
	_, err := game.service.Register(context.Background(), manual, batch(first, false, attack(t, third, 10_000)))
	assertProblem(t, err, 409, services.InsufficientSoldiersProblemType, "")
	joined := game.register(t, manual, batch(first, false, attack(t, third, 1)))
	if joined.GenerationID != third.GenerationID {
		t.Fatalf("a batch two turns late answered generation %d, want the open %d", joined.GenerationID, third.GenerationID)
	}
	fourth := game.toTurnEnd(t, manual)
	if !hasGroup(fourth, first.TeamColor, 1) {
		t.Fatalf("the batch two turns late wasn't played: %+v", fourth.Generation.SoldierGroups)
	}

	// Three turns late, it is stale, and says how late a batch may be.
	_, err = game.service.Register(context.Background(), manual, batch(first, false, attack(t, fourth, 1)))
	assertProblem(t, err, 409, services.StaleGenerationProblemType, "generation is no longer current")
	assertProblem(t, err, 409, services.StaleGenerationProblemType, "2 s after the previous one")
	assertProblem(t, err, 409, services.StaleGenerationProblemType, "one of the 2 turns before it")
	// The second turn's batch is two turns late, and still joins.
	game.register(t, manual, batch(second, false))
}

// A late batch whose commands no longer fit the open board is refused for
// them, and the turn its request played still commits.
func TestALateBatchThatNoLongerFitsIsRefusedAfterTheTurnPlays(t *testing.T) {
	game := startClockGame(t, ``, services.ServerBot{Manual: true}, services.ServerBot{Level: 2})
	manual := game.players[0]
	first := game.current(t, manual)
	*game.clock = game.turnEnd(t).Add(time.Millisecond)
	_, err := game.service.Register(context.Background(), manual, batch(first, false, attack(t, first, 10_000)))
	assertProblem(t, err, 409, services.InsufficientSoldiersProblemType, "")
	if game.generationCount(t) != 2 {
		t.Fatalf("the refused late batch rolled back the turn its request played: %d generations", game.generationCount(t))
	}
}

// A late batch that completes its turn completes the open one, with its
// memory: the client answered a turn late, and its next answer is for the
// turn after the open one. Once the open turn is complete, a late batch is
// stale.
func TestALateCompletingBatchCompletesTheOpenTurn(t *testing.T) {
	game := startClockGame(t, ``, services.ServerBot{Manual: true}, services.ServerBot{})
	bot := game.players[1]
	first := game.current(t, bot)
	*game.clock = game.turnEnd(t).Add(300 * time.Millisecond)
	answer := batch(first, true, attack(t, first, 4))
	answer.Memory = models.BotMemory(`{"turn":1}`)
	second := game.register(t, bot, answer)
	if second.TurnNumber != 2 || !second.Submitted || string(second.Memory) != `{"turn":1}` {
		t.Fatalf("after the late answer: turn %d, submitted %v, memory %s", second.TurnNumber, second.Submitted, second.Memory)
	}
	// The turn still waits for its end: the hand player never completes.
	if game.generationCount(t) != 2 {
		t.Fatalf("the late answer played the open turn: %d generations", game.generationCount(t))
	}
	_, err := game.service.Register(context.Background(), bot, batch(first, true))
	assertProblem(t, err, 409, services.StaleGenerationProblemType, "")
	_, err = game.service.Register(context.Background(), bot, batch(second, true))
	assertTurnStatus(t, err, 409)
	third := game.toTurnEnd(t, game.players[0])
	if !hasGroup(third, first.TeamColor, 4) {
		t.Fatalf("the late answer wasn't played: %+v", third.Generation.SoldierGroups)
	}
}

func TestARetryOfAnAcceptedBatchAfterItsTurnEndedGetsItsState(t *testing.T) {
	game := startClockGame(t, ``, services.ServerBot{Manual: true}, services.ServerBot{Level: 2})
	manual := game.players[0]
	first := game.current(t, manual)
	accepted := batch(first, false, attack(t, first, 3))
	game.register(t, manual, accepted)
	*game.clock = game.turnEnd(t).Add(time.Millisecond)
	// The retry is the request that finds the turn over: it plays the turn
	// once and answers the state, and isn't applied again in the open turn.
	for range 2 {
		state := game.register(t, manual, accepted)
		if state.TurnNumber != 2 || !hasGroup(state, first.TeamColor, 3) || game.generationCount(t) != 2 {
			t.Fatalf("retry = turn %d, %d generations", state.TurnNumber, game.generationCount(t))
		}
	}
	// A new batch for the ended turn joins the open turn; its retries, in
	// this turn and after it played, aren't applied again.
	late := batch(first, false, attack(t, first, 2))
	game.register(t, manual, late)
	game.register(t, manual, late)
	third := game.toTurnEnd(t, manual)
	game.register(t, manual, late)
	fourth := game.toTurnEnd(t, manual)
	twos := 0
	for _, group := range fourth.Generation.SoldierGroups {
		if group.TeamColor == first.TeamColor && group.Soldiers == 2 {
			twos++
		}
	}
	if !hasGroup(third, first.TeamColor, 2) || twos > 1 {
		t.Fatalf("the late batch was played %d times: %+v", twos, fourth.Generation.SoldierGroups)
	}
}

func TestARealTimeTurnSendsAHostedBotItsNextTurn(t *testing.T) {
	game := startClockGame(t, ``, services.ServerBot{Manual: true}, services.ServerBot{URL: "https://bot.example.org/turn"})
	firstTurns := game.runners.take()
	if len(firstTurns) != 1 {
		t.Fatalf("the start sent %d turns, want the hosted bot's", len(firstTurns))
	}
	first := game.open(t)
	*game.clock = game.turnEnd(t)
	game.applyDeadline(t, game.players[0])
	next := game.open(t)
	if next.ID == first.ID {
		t.Fatal("the deadline call played nothing")
	}
	sent := game.runners.take()
	if len(sent) != 1 || sentTo(t, game.tokens, sent[0].event) != game.players[1].PlayerID {
		t.Fatalf("sent %+v, want the hosted bot's next turn", sent)
	}
	delivery := sent[0].event.Delivery
	wantDeadline := next.CreatedAt.Add(time.Duration(models.DefaultTurnPaceMilliseconds) * time.Millisecond)
	if !sent[0].committed || delivery.Turn.Generation.ID != next.ID || delivery.Submit.DeadlineAt == nil || !delivery.Submit.DeadlineAt.Equal(wantDeadline) ||
		delivery.Turn.TurnDeadlineAt == nil || !delivery.Turn.TurnDeadlineAt.Equal(wantDeadline) {
		t.Fatalf("the hosted bot's next turn = %+v", delivery)
	}
	// The bot's answer to the turn it missed joins the open turn, and
	// completes it.
	claims, err := game.tokens.Verify(firstTurns[0].event.Delivery.Submit.Token, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	complete := true
	late := func() (models.GenerationState, error) {
		return game.service.Register(context.Background(), domain.Principal{PlayerID: claims.PlayerID}, models.RegisterGenerationCommands{
			GameID: claims.GameID, GenerationID: claims.GenerationID, RequestID: uuid.NewString(), Complete: &complete, Commands: []models.PublicGenerationCommand{}})
	}
	if state, err := late(); err != nil || state.GenerationID != next.ID || !state.Submitted {
		t.Fatalf("the late answer = %+v, %v", state, err)
	}
	// Three turns later, it is stale.
	for range 2 {
		*game.clock = game.turnEnd(t)
		game.applyDeadline(t, game.players[0])
	}
	_, err = late()
	assertProblem(t, err, 409, services.StaleGenerationProblemType, "")
}

func TestTheReadsOfARealTimeGamePlayAnOverdueTurn(t *testing.T) {
	game := startClockGame(t, ``, services.ServerBot{Manual: true}, services.ServerBot{Level: 2})
	manual := game.players[0]
	ctx := context.Background()
	reads := []struct {
		name string
		read func() error
	}{
		{"replay", func() error { _, err := game.service.Replay(ctx, manual, game.lobbyID); return err }},
		{"logs", func() error { _, err := game.service.Logs(ctx, manual, game.lobbyID, game.open(t).ID); return err }},
		{"demo", func() error { _, err := game.service.Demo(ctx, manual, game.lobbyID); return err }},
		{"watch", func() error { _, err := game.service.Watch(ctx, manual, game.lobbyID); return err }},
		{"watch replay", func() error { _, err := game.service.WatchReplay(ctx, manual, game.lobbyID); return err }},
		{"watch demo", func() error { _, err := game.service.WatchDemo(ctx, manual, game.lobbyID); return err }},
		{"watch logs", func() error { _, err := game.service.WatchLogs(ctx, manual, game.lobbyID, game.open(t).ID); return err }},
		{"turn delivery", func() error {
			delivery, err := game.service.TurnDelivery(ctx, manual, game.lobbyID, "", game.tokens, serverBotSubmitURL)
			if err == nil && delivery.Turn.Generation.ID != game.open(t).ID {
				return fmt.Errorf("delivered generation %d, not the open one", delivery.Turn.Generation.ID)
			}
			return err
		}},
	}
	for _, read := range reads {
		before := game.open(t).ID
		*game.clock = game.turnEnd(t)
		if err := read.read(); err != nil {
			t.Fatalf("%s: %v", read.name, err)
		}
		after := game.open(t)
		if after.ID == before {
			t.Fatalf("%s didn't play the overdue turn", read.name)
		}
		if events := game.eventsOf(t, after.ID); events != 2 {
			t.Fatalf("%s pushed the new generation %d times", read.name, events)
		}
		// Before the next turn's end, a read changes nothing.
		if err := read.read(); err != nil || game.open(t).ID != after.ID {
			t.Fatalf("%s again: %v, generation %d", read.name, err, game.open(t).ID)
		}
	}
}

func TestAGameOfBotsOnlyStoresNoPaceAndTimesOut(t *testing.T) {
	game := startClockGame(t, `{"turn_timeout_seconds":5,"turn_pace_milliseconds":500}`, services.ServerBot{}, services.ServerBot{Level: 2})
	localhost := game.players[0]
	state := game.current(t, localhost)
	if game.stored(t).TurnPaceMilliseconds != nil || state.TurnPaceMilliseconds != nil {
		t.Fatalf("a game of bots stored a pace: %v", state.TurnPaceMilliseconds)
	}
	if want := game.open(t).CreatedAt.Add(5 * time.Second); state.TurnDeadlineAt == nil || !state.TurnDeadlineAt.Equal(want) {
		t.Fatalf("turn_deadline_at = %v, want %v", state.TurnDeadlineAt, want)
	}
	// The pace means nothing here: the turn waits for the localhost bot.
	*game.clock = game.open(t).CreatedAt.Add(time.Second)
	if waiting := game.applyDeadline(t, localhost); waiting.GenerationID != state.GenerationID || waiting.Outcome != nil {
		t.Fatalf("after a pace = %+v", waiting)
	}
	*game.clock = *state.TurnDeadlineAt
	ended := game.applyDeadline(t, localhost)
	if ended.Outcome == nil || ended.Outcome.Reason != models.GameOutcomeTurnTimeout || ended.GenerationID != state.GenerationID {
		t.Fatalf("after the turn limit = %+v", ended.Outcome)
	}
	// A late batch of a game that waits is refused as before.
	_, err := game.service.Register(context.Background(), localhost, batch(state, true))
	var rejection *services.GenerationError
	if !errors.As(err, &rejection) || rejection.Type != services.GameEndedProblemType {
		t.Fatalf("late batch = %v", err)
	}
}

func TestGoBotsPlayOnAfterARealTimeTurnLeavesOnlyThem(t *testing.T) {
	services.SetGoBotTimeBudgets(t, 0, 0)
	game := startClockGame(t, ``, services.ServerBot{Manual: true}, services.ServerBot{Level: 1}, services.ServerBot{Level: 1})
	manual := game.players[0]
	eliminateOnTheClocksTurn(t, game)

	*game.clock = game.turnEnd(t)
	state := game.applyDeadline(t, manual)
	if state.TurnNumber != 2 || state.Outcome != nil {
		t.Fatalf("the clock's turn = %+v", state)
	}
	for _, castle := range state.Generation.Castles {
		if castle.TeamColor == 1 {
			t.Fatal("the manual player's team still owns a castle")
		}
	}
	for _, group := range state.Generation.SoldierGroups {
		if group.TeamColor == 1 {
			t.Fatal("the manual player's group is still on the field")
		}
	}
	// Only Go bots are left, so they played on after the clock's turn: one
	// more turn, with this test's budget.
	if count := game.generationCount(t); count != 3 {
		t.Fatalf("the game has %d generations, want 3", count)
	}
	// The Go bots' new turn is complete, so the next request plays it,
	// whatever the clock says.
	if next := game.current(t, manual); next.TurnNumber != 4 {
		t.Fatalf("the next request reached turn %d", next.TurnNumber)
	}
}

// eliminateOnTheClocksTurn sets up the open turn of a game whose first
// player is manual so that the turn the clock plays eliminates that player:
// their team has no castle left, and its last group, one soldier, reaches a
// castle of 30 in this turn.
func eliminateOnTheClocksTurn(t *testing.T, game clockGame) {
	t.Helper()
	first := game.open(t)
	var origin, target models.Castle
	for _, castle := range first.Map.Castles {
		switch castle.TeamColor {
		case 1:
			origin = castle
		case 2:
			target = castle
		}
	}
	if err := game.database.Model(&models.Castle{}).Where(&models.Castle{Model: gorm.Model{ID: origin.ID}}).Update("TeamColor", models.NeutralTeamColor).Error; err != nil {
		t.Fatal(err)
	}
	group := models.SoldierGroup{SoldierGroupFields: models.SoldierGroupFields{OriginCastleID: origin.ID, DestinationCastleID: target.ID, SoldierAmount: 1},
		TeamColor: 1, GenerationID: first.ID, Position: pgtype.Point{P: target.Position.P, Valid: true}}
	testutils.CreateSoldierGroupForTest(t, game.database, &group)
}

// A state read that plays the clock's turn, which leaves only Go bots, lets
// them play on for the read budget only, not the write budget.
func TestAStateReadThatLeavesOnlyGoBotsKeepsToTheReadBudget(t *testing.T) {
	services.SetGoBotTimeBudgets(t, 3*time.Second, 0)
	game := startClockGame(t, `{"max_generations":1200}`, services.ServerBot{Manual: true}, services.ServerBot{Level: 1}, services.ServerBot{Level: 1})
	manual := game.players[0]
	eliminateOnTheClocksTurn(t, game)
	*game.clock = game.turnEnd(t)
	started := time.Now()
	state := game.current(t, manual)
	if elapsed := time.Since(started); elapsed > 1500*time.Millisecond {
		t.Fatalf("the read took %s", elapsed)
	}
	// The clock's turn, then one turn of the Go bots: a zero budget plays
	// one turn per request.
	if state.TurnNumber != 2 || game.generationCount(t) != 3 {
		t.Fatalf("the read answered turn %d, and the game has %d generations", state.TurnNumber, game.generationCount(t))
	}
}
