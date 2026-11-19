package services_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/INTODAN/mpl/domain"
	"islandBattles/backend/dal"
	"islandBattles/backend/internal/testutils"
	"islandBattles/backend/models"
	"islandBattles/backend/services"
	"islandBattles/backend/submittoken"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// The clock games name their teams Blue (colour 1), Red (2) and Green (3),
// and their players Player 1, 2 and 3.
const (
	blue  = uint(1)
	red   = uint(2)
	green = uint(3)
)

func (game clockGame) resign(t *testing.T, player domain.Principal) models.GenerationState {
	t.Helper()
	state, err := game.service.Resign(context.Background(), player, game.lobbyID)
	if err != nil {
		t.Fatal(err)
	}
	return state
}

// toTurnEnd moves the clock to the open turn's end and has player call the
// deadline there, as a hand player's client does once per turn.
func (game clockGame) toTurnEnd(t *testing.T, player domain.Principal) models.GenerationState {
	t.Helper()
	*game.clock = game.turnEnd(t)
	return game.applyDeadline(t, player)
}

// expectProblem is nil for a 409 refusal with the problem type, and an error
// for anything else.
func expectProblem(err error, problemType string) error {
	var rejection *services.GenerationError
	if errors.As(err, &rejection) && rejection.Status == 409 && rejection.Type == problemType {
		return nil
	}
	return fmt.Errorf("error = %v, want 409 %s", err, problemType)
}

// castleOf returns the team's castle in a state, or nil once it has none.
func castleOf(state models.GenerationState, team uint) *models.CastleView {
	for index, castle := range state.Generation.Castles {
		if castle.TeamColor == team {
			return &state.Generation.Castles[index]
		}
	}
	return nil
}

func groupsOf(state models.GenerationState, team uint) int {
	count := 0
	for _, group := range state.Generation.SoldierGroups {
		if group.TeamColor == team {
			count++
		}
	}
	return count
}

func turnNumber(number int) *int { return &number }

func castleByID(state models.GenerationState, id uint) *models.CastleView {
	for index, castle := range state.Generation.Castles {
		if castle.ID == id {
			return &state.Generation.Castles[index]
		}
	}
	return nil
}

// setCastles sets a column of the team's castles in the open generation.
func setCastles(t *testing.T, game clockGame, team uint, column string, value any) {
	t.Helper()
	for _, castle := range game.open(t).Map.Castles {
		if castle.TeamColor == team {
			if err := game.database.Model(&models.Castle{}).Where(&models.Castle{Model: gorm.Model{ID: castle.ID}}).Update(column, value).Error; err != nil {
				t.Fatal(err)
			}
		}
	}
}

// withMoreCastles is a two-team map whose first team, Blue, owns two castles,
// so that it wins on castles.
func withMoreCastles(settings string) string {
	fields := map[string]any{}
	if err := json.Unmarshal([]byte(settings), &fields); err != nil {
		panic(err)
	}
	circle := testutils.CircleMap(2)
	extra := circle.Castles[0]
	extra.Y += 1.5
	circle.Castles = append(circle.Castles, extra)
	fields["map"] = circle
	raw, err := json.Marshal(fields)
	if err != nil {
		panic(err)
	}
	return string(raw)
}

// Two teams: Red resigns, nothing changes until the open turn plays, and that
// turn ends the game: Red's castle turns neutral with its soldiers, Red's own
// command of the turn is dropped, and Blue wins.
func TestAResignationEndsATwoTeamGameAtTheNextTurn(t *testing.T) {
	game := startClockGame(t, ``, services.ServerBot{Manual: true}, services.ServerBot{Manual: true})
	bluePlayer, redPlayer := game.players[0], game.players[1]
	first := game.current(t, redPlayer)
	game.register(t, redPlayer, batch(first, false, attack(t, first, 5)))

	state := game.resign(t, redPlayer)
	if state.GenerationID != first.GenerationID || state.Outcome != nil || castleOf(state, red) == nil || len(state.Departures) != 0 {
		t.Fatalf("the resignation's answer = %+v", state)
	}
	// Again: nothing changes.
	pushed := len(game.publisher.Publications())
	if again := game.resign(t, redPlayer); again.GenerationID != first.GenerationID {
		t.Fatalf("a second resignation = %+v", again)
	}
	if departures := game.stored(t).Departures; len(departures) != 1 || departures[0].Turn != nil || departures[0].Kind != models.DepartureResigned {
		t.Fatalf("stored departures = %+v", departures)
	}
	if len(game.publisher.Publications()) != pushed {
		t.Fatal("a resignation pushed an event before its turn")
	}
	// Red's later commands aren't played.
	_, err := game.service.Register(context.Background(), redPlayer, batch(first, false, attack(t, first, 3)))
	assertProblem(t, err, 409, services.TeamOutProblemType, "")

	// The turn waits for Blue, a hand player, until its end.
	ended := game.toTurnEnd(t, bluePlayer)
	want := models.GameOutcomeView{Reason: models.GameOutcomeLastTeamStanding, WinnerTeamColor: colour(blue), Summary: "Red resigned; Blue wins as the last team standing",
		Departures: []models.DepartureView{{TeamColor: red, Kind: models.DepartureResigned, Players: []string{"Player 2"}, TurnNumber: turnNumber(2)}}}
	if ended.Outcome == nil || !reflect.DeepEqual(*ended.Outcome, want) {
		t.Fatalf("outcome = %+v, want %+v", ended.Outcome, want)
	}
	if castleOf(ended, red) != nil || groupsOf(ended, red) != 0 {
		t.Fatalf("Red is still on the board: %+v", ended.Generation)
	}
	for _, castle := range ended.Generation.Castles {
		if castle.TeamColor == models.NeutralTeamColor && castle.Soldiers != 30 {
			t.Fatalf("Red's castle turned neutral with %d soldiers, want its 30", castle.Soldiers)
		}
	}
	if len(ended.Departures) != 1 || ended.Departures[0].TeamColor != red {
		t.Fatalf("state departures = %+v", ended.Departures)
	}
	if events := len(game.publisher.GameEndedEvents(t)); events != 2 {
		t.Fatalf("pushed game.ended %d times, want once per player", events)
	}
	_, err = game.service.Resign(context.Background(), redPlayer, game.lobbyID)
	assertProblem(t, err, 409, services.GameEndedProblemType, "")
	game.applyDeadline(t, bluePlayer)
	if events := len(game.publisher.GameEndedEvents(t)); events != 2 {
		t.Fatalf("a later request pushed game.ended again: %d", events)
	}
}

// Against bots that have already played the turn, the turn no longer waits
// for the team that resigns, so it plays in the resign request itself.
func TestAResignationAgainstBotsPlaysTheTurnAtOnce(t *testing.T) {
	game := startClockGame(t, ``, services.ServerBot{Manual: true}, services.ServerBot{Level: 1})
	first := game.current(t, game.players[0])
	state := game.resign(t, game.players[0])
	if state.GenerationID == first.GenerationID || state.Outcome == nil || state.Outcome.Reason != models.GameOutcomeLastTeamStanding ||
		!strings.HasPrefix(state.Outcome.Summary, "Blue resigned; Red wins") {
		t.Fatalf("the resignation's answer = %+v, outcome %+v", state, state.Outcome)
	}
	// The lobby can be reopened at once.
	ended, err := services.EndAbandonedRound(context.Background(), game.database, nil, game.lobbyID, 1, *game.clock)
	if err != nil || !ended {
		t.Fatalf("round ended = %v, %v", ended, err)
	}

	// A hosted bot's completing batch plays a turn that only waited for it.
	hosted := startClockGame(t, ``, services.ServerBot{Manual: true}, services.ServerBot{URL: "https://bot.example.org/turn"})
	first = hosted.current(t, hosted.players[0])
	hosted.resign(t, hosted.players[0])
	if hosted.open(t).ID != first.GenerationID {
		t.Fatal("the turn played before the hosted bot answered")
	}
	turns := hosted.runners.take()
	claims, err := hosted.tokens.Verify(turns[0].event.Delivery.Submit.Token, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	answer := hosted.register(t, domain.Principal{PlayerID: claims.PlayerID}, batch(first, true))
	if answer.Outcome == nil || answer.Outcome.WinnerTeamColor == nil || *answer.Outcome.WinnerTeamColor != red {
		t.Fatalf("the hosted bot's answer = %+v", answer.Outcome)
	}
}

// Three teams: after Red resigns, the next generation has Red's castle
// neutral with its soldiers and no burst debt, no Red group, and the game
// goes on. Red's player gets 409 for later requests, and every state, the
// replay and the demo name Red as resigned.
func TestAResignedTeamsCastlesTurnNeutralInAThreeTeamGame(t *testing.T) {
	game := startClockGame(t, ``, services.ServerBot{Manual: true}, services.ServerBot{Manual: true}, services.ServerBot{Manual: true})
	bluePlayer, redPlayer := game.players[0], game.players[1]
	ctx := context.Background()
	first := game.current(t, redPlayer)
	game.register(t, redPlayer, batch(first, false, attack(t, first, 4)))
	second := game.toTurnEnd(t, bluePlayer)
	if groupsOf(second, red) != 1 {
		t.Fatalf("Red's group isn't on the field: %+v", second.Generation.SoldierGroups)
	}
	redCastle := castleOf(second, red)
	// Red's castle owes a burst.
	setCastles(t, game, red, "BurstDebt", 3)
	redState := game.current(t, redPlayer)
	game.register(t, redPlayer, batch(redState, false, attack(t, redState, 2)))
	game.resign(t, redPlayer)

	third := game.toTurnEnd(t, bluePlayer)
	if third.Outcome != nil {
		t.Fatalf("the game ended: %+v", third.Outcome)
	}
	if castleOf(third, red) != nil || groupsOf(third, red) != 0 {
		t.Fatalf("Red is still on the board: %+v", third.Generation)
	}
	neutral, before := castleByID(third, redCastle.ID), castleByID(redState, redCastle.ID)
	// Red's castle kept the soldiers it had; its last send was dropped, and
	// a neutral castle doesn't grow.
	if neutral == nil || before == nil || before.BurstDebt != 3 || neutral.TeamColor != models.NeutralTeamColor || neutral.Soldiers != before.Soldiers || neutral.BurstDebt != 0 {
		t.Fatalf("Red's castle = %+v, it had %+v", neutral, before)
	}
	want := []models.DepartureView{{TeamColor: red, Kind: models.DepartureResigned, Players: []string{"Player 2"}, TurnNumber: turnNumber(3)}}
	if !reflect.DeepEqual(third.Departures, want) {
		t.Fatalf("departures = %+v, want %+v", third.Departures, want)
	}
	watch, err := game.service.Watch(ctx, bluePlayer, game.lobbyID)
	if err != nil || !reflect.DeepEqual(watch.Departures, want) {
		t.Fatalf("watch departures = %+v, %v", watch.Departures, err)
	}

	// Red's player is out.
	latest := game.current(t, redPlayer)
	_, err = game.service.Register(ctx, redPlayer, batch(latest, true))
	assertProblem(t, err, 409, services.TeamOutProblemType, "")
	_, err = game.service.Resign(ctx, redPlayer, game.lobbyID)
	assertProblem(t, err, 409, services.TeamOutProblemType, "")
	_, err = game.service.TurnDelivery(ctx, redPlayer, game.lobbyID, "", game.tokens, serverBotSubmitURL)
	assertProblem(t, err, 409, services.TeamOutProblemType, "")

	// When the game ends, the replay and the demo carry it.
	setCastles(t, game, green, "TeamColor", models.NeutralTeamColor)
	ended := game.toTurnEnd(t, bluePlayer)
	if ended.Outcome == nil || ended.Outcome.Summary != "Red resigned; Blue wins as the last team standing" || !reflect.DeepEqual(ended.Outcome.Departures, want) {
		t.Fatalf("outcome = %+v", ended.Outcome)
	}
	replay, err := game.service.Replay(ctx, bluePlayer, game.lobbyID)
	if err != nil || !reflect.DeepEqual(replay.Outcome.Departures, want) {
		t.Fatalf("replay outcome = %+v, %v", replay.Outcome, err)
	}
	demo, err := game.service.Demo(ctx, bluePlayer, game.lobbyID)
	if err != nil || !reflect.DeepEqual(demo.Outcome.Departures, want) || demo.Outcome.Summary != ended.Outcome.Summary {
		t.Fatalf("demo outcome = %+v, %v", demo.Outcome, err)
	}
}

// Both teams resigning in one turn is a draw.
func TestBothTeamsResigningInOneTurnIsADraw(t *testing.T) {
	game := startClockGame(t, ``, services.ServerBot{Manual: true}, services.ServerBot{Manual: true})
	game.resign(t, game.players[0])
	state := game.resign(t, game.players[1])
	if state.Outcome == nil || state.Outcome.Reason != models.GameOutcomeAllEliminated || state.Outcome.WinnerTeamColor != nil ||
		state.Outcome.Summary != "Blue resigned; Red resigned; draw: no team is left" {
		t.Fatalf("outcome = %+v", state.Outcome)
	}
}

// A team that resigned and keeps calling, and a team that went silent, leave
// in the same turn: a draw, not an abandoned game, since the server still
// heard from the team that resigned.
func TestAResigningTeamAndASilentTeamLeaveTogether(t *testing.T) {
	game := startClockGame(t, `{"turn_timeout_seconds":10}`, services.ServerBot{Manual: true}, services.ServerBot{Manual: true})
	redPlayer := game.players[1]
	started := game.open(t).CreatedAt
	// Red calls each turn's end until the open turn ends more than 10 s
	// after Blue was last heard from.
	for game.turnEnd(t).Sub(started) <= 10*time.Second {
		game.toTurnEnd(t, redPlayer)
	}
	game.resign(t, redPlayer)
	state := game.toTurnEnd(t, redPlayer)
	want := "Red resigned; Blue left the game: Player 1 didn't respond for 10 s; draw: no team is left"
	if state.Outcome == nil || state.Outcome.Reason != models.GameOutcomeAllEliminated || state.Outcome.Summary != want {
		t.Fatalf("outcome = %+v, want %q", state.Outcome, want)
	}
}

// When a team resigned earlier and the last two teams are eliminated later
// in the same turn, the draw says so, as without the resignation.
func TestAnEarlierResignationDoesNotChangeHowTheLastTeamsWent(t *testing.T) {
	game := startClockGame(t, ``, services.ServerBot{Manual: true}, services.ServerBot{Manual: true}, services.ServerBot{Manual: true})
	bluePlayer := game.players[0]
	game.resign(t, game.players[1])
	game.toTurnEnd(t, bluePlayer)
	setCastles(t, game, blue, "TeamColor", models.NeutralTeamColor)
	setCastles(t, game, green, "TeamColor", models.NeutralTeamColor)
	state := game.toTurnEnd(t, bluePlayer)
	want := "Red resigned; draw: the last teams were eliminated in the same turn"
	if state.Outcome == nil || state.Outcome.Reason != models.GameOutcomeAllEliminated || state.Outcome.Summary != want {
		t.Fatalf("outcome = %+v, want %q", state.Outcome, want)
	}
}

// A manual player who makes no request for longer than the turn limit, while
// the other keeps calling the deadline and sends no command, is taken out in
// the next turn's end; the caller stays.
func TestASilentPlayerLeavesAndADeadlineCallerStays(t *testing.T) {
	game := startClockGame(t, `{"turn_timeout_seconds":10}`, services.ServerBot{Manual: true}, services.ServerBot{Manual: true})
	bluePlayer := game.players[0]
	started := game.open(t).CreatedAt
	var state models.GenerationState
	for game.turnEnd(t).Sub(started) <= 10*time.Second {
		state = game.toTurnEnd(t, bluePlayer)
		if state.Outcome != nil || castleOf(state, red) == nil {
			t.Fatalf("Red left at turn %d, %s after the start", state.TurnNumber, game.open(t).CreatedAt.Sub(started))
		}
	}
	// This turn ends more than 10 s after Red was last heard from.
	state = game.toTurnEnd(t, bluePlayer)
	want := "Red left the game: Player 2 didn't respond for 10 s; Blue wins as the last team standing"
	if state.Outcome == nil || state.Outcome.Summary != want || state.Outcome.WinnerTeamColor == nil || *state.Outcome.WinnerTeamColor != blue {
		t.Fatalf("outcome = %+v, want %q", state.Outcome, want)
	}
	if departures := state.Outcome.Departures; len(departures) != 1 || departures[0].Kind != models.DepartureLeft || !reflect.DeepEqual(departures[0].Players, []string{"Player 2"}) {
		t.Fatalf("departures = %+v", departures)
	}
	// The lobby's round-ended hook answers that the round is over.
	if ended, err := services.EndAbandonedRound(context.Background(), game.database, nil, game.lobbyID, 1, *game.clock); err != nil || !ended {
		t.Fatalf("round ended = %v, %v", ended, err)
	}
}

// A player who comes back after nearly the limit, and whose own request ends
// the turn, is never taken out by it.
func TestAPlayerWhoComesBackIsNotTakenOutByTheirOwnRequest(t *testing.T) {
	game := startClockGame(t, `{"turn_timeout_seconds":10}`, services.ServerBot{Manual: true}, services.ServerBot{Manual: true})
	bluePlayer, redPlayer := game.players[0], game.players[1]
	started := game.open(t).CreatedAt
	for game.turnEnd(t).Add(time.Second).Sub(started) <= 10*time.Second {
		game.toTurnEnd(t, bluePlayer)
	}
	// The next turn would end more than 10 s after Red was last heard from,
	// if Red's own call weren't the one that ends it.
	for range 15 {
		state := game.toTurnEnd(t, redPlayer)
		if state.Outcome != nil || castleOf(state, red) == nil {
			t.Fatalf("Red was taken out by their own request: %+v", state.Outcome)
		}
		game.applyDeadline(t, bluePlayer)
	}
}

// A client that calls once per turn is never taken out, even with the
// shortest limit and the longest pace, where its calls are a pace apart: the
// silence limit is then one pace plus 5 s.
func TestAPlayerCallingOncePerTurnIsNeverSilent(t *testing.T) {
	game := startClockGame(t, `{"turn_timeout_seconds":5,"turn_pace_milliseconds":5000}`, services.ServerBot{Manual: true}, services.ServerBot{Manual: true})
	bluePlayer, redPlayer := game.players[0], game.players[1]
	for turn := range 12 {
		// Red's client calls half a second before the turn's end, by its own
		// clock, which plays nothing; Blue's call at the end plays the turn.
		*game.clock = game.turnEnd(t).Add(-500 * time.Millisecond)
		game.applyDeadline(t, redPlayer)
		state := game.toTurnEnd(t, bluePlayer)
		if state.Outcome != nil || castleOf(state, red) == nil {
			t.Fatalf("turn %d: Red was taken out: %+v", turn, state.Outcome)
		}
	}
}

// A game that no request touches for longer than the turn limit ends as
// abandoned on its open generation at the next request, whoever makes it and
// whichever request it is, with the most castles winning, also when only Go
// bots are left to play it. A second request changes nothing.
func TestAnUntouchedGameIsAbandonedOnItsOpenGeneration(t *testing.T) {
	ctx := context.Background()
	requests := map[string]func(game clockGame, open models.GenerationState) error{
		"state read": func(game clockGame, _ models.GenerationState) error {
			_, err := game.service.Current(ctx, game.players[1], game.lobbyID)
			return err
		},
		"deadline": func(game clockGame, _ models.GenerationState) error {
			_, err := game.service.ApplyDeadline(ctx, game.players[1], game.lobbyID)
			return err
		},
		"watch": func(game clockGame, _ models.GenerationState) error {
			_, err := game.service.Watch(ctx, game.players[1], game.lobbyID)
			return err
		},
		"replay": func(game clockGame, _ models.GenerationState) error {
			_, err := game.service.Replay(ctx, game.players[1], game.lobbyID)
			return err
		},
		// The refused batch still commits the ending.
		"batch": func(game clockGame, open models.GenerationState) error {
			_, err := game.service.Register(ctx, game.players[1], batch(open, true))
			return expectProblem(err, services.GameEndedProblemType)
		},
		"turn delivery": func(game clockGame, _ models.GenerationState) error {
			_, err := game.service.TurnDelivery(ctx, game.players[1], game.lobbyID, "", game.tokens, serverBotSubmitURL)
			return expectProblem(err, services.GameEndedProblemType)
		},
		"resign": func(game clockGame, _ models.GenerationState) error {
			_, err := game.service.Resign(ctx, game.players[1], game.lobbyID)
			return expectProblem(err, services.GameEndedProblemType)
		},
	}
	for name, request := range requests {
		t.Run(name, func(t *testing.T) {
			game := startClockGame(t, withMoreCastles(`{"turn_timeout_seconds":5}`), services.ServerBot{Level: 1}, services.ServerBot{Manual: true})
			open := game.current(t, game.players[1])
			generations := game.generationCount(t)
			*game.clock = game.turnEnd(t).Add(5*time.Second + time.Millisecond)
			if err := request(game, open); err != nil {
				t.Fatal(err)
			}
			stored := game.stored(t)
			if stored.Outcome == nil || stored.Outcome.Reason != models.GameOutcomeAbandoned || stored.Outcome.WinnerTeamColor == nil || *stored.Outcome.WinnerTeamColor != blue {
				t.Fatalf("outcome = %+v", stored.Outcome)
			}
			if game.open(t).ID != open.GenerationID || game.generationCount(t) != generations {
				t.Fatal("a turn was played")
			}
			state := game.current(t, game.players[1])
			if state.Outcome.Summary != "The game was abandoned: nobody played for 5 s; Blue wins with the most castles" {
				t.Fatalf("summary = %q", state.Outcome.Summary)
			}
			if events := len(game.publisher.GameEndedEvents(t)); events != 2 {
				t.Fatalf("pushed game.ended %d times", events)
			}
			game.applyDeadline(t, game.players[1])
			if events := len(game.publisher.GameEndedEvents(t)); events != 2 || game.generationCount(t) != generations {
				t.Fatal("a second request changed the game")
			}
		})
	}

	// Once the hand player is out and only Go bots play, an untouched game is
	// still abandoned, not caught up.
	game := startClockGame(t, `{"turn_timeout_seconds":5}`, services.ServerBot{Manual: true}, services.ServerBot{Level: 1}, services.ServerBot{Level: 1})
	services.SetGoBotTimeBudgets(t, 0, 0)
	game.resign(t, game.players[0])
	generations := game.generationCount(t)
	*game.clock = game.turnEnd(t).Add(5*time.Second + time.Millisecond)
	if _, err := game.service.Watch(ctx, game.players[1], game.lobbyID); err != nil {
		t.Fatal(err)
	}
	if stored := game.stored(t); stored.Outcome == nil || stored.Outcome.Reason != models.GameOutcomeAbandoned || game.generationCount(t) != generations {
		t.Fatalf("outcome = %+v after %d more generations", stored.Outcome, game.generationCount(t)-generations)
	}
}

// When every team still in the game went silent, and a request that isn't
// theirs, such as a watcher's, plays the turn, the game is abandoned rather
// than a draw of teams that all left; a team whose resignation waits can't
// win it, and the summary says it resigned.
func TestEveryoneSilentIsAbandonedAndAPendingResignationIsNamed(t *testing.T) {
	game := startClockGame(t, withMoreCastles(`{"turn_timeout_seconds":5}`), services.ServerBot{Manual: true}, services.ServerBot{Manual: true})
	game.resign(t, game.players[0])
	// The hook answers that the round is running until the game is abandoned.
	if ended, err := services.EndAbandonedRound(context.Background(), game.database, game.publisher, game.lobbyID, 1, game.turnEnd(t).Add(5*time.Second)); err != nil || ended {
		t.Fatalf("round ended = %v, %v", ended, err)
	}
	ended, err := services.EndAbandonedRound(context.Background(), game.database, game.publisher, game.lobbyID, 1, game.turnEnd(t).Add(5*time.Second+time.Millisecond))
	if err != nil || !ended {
		t.Fatalf("round ended = %v, %v", ended, err)
	}
	stored := game.stored(t)
	if stored.Outcome.Reason != models.GameOutcomeAbandoned || stored.Outcome.WinnerTeamColor == nil || *stored.Outcome.WinnerTeamColor != red {
		t.Fatalf("outcome = %+v", stored.Outcome)
	}
	state := game.current(t, game.players[1])
	if want := "Blue resigned; the game was abandoned: nobody played for 5 s; Red wins with the most castles"; state.Outcome.Summary != want {
		t.Fatalf("summary = %q, want %q", state.Outcome.Summary, want)
	}
	if departures := state.Outcome.Departures; len(departures) != 1 || departures[0].TurnNumber != nil {
		t.Fatalf("departures = %+v", departures)
	}
	if events := len(game.publisher.GameEndedEvents(t)); events != 2 {
		t.Fatalf("pushed game.ended %d times", events)
	}

	// When a watcher's reads play the turns after both players closed their
	// tabs, nobody is heard: the game is abandoned, not a draw of two teams
	// that both left.
	silent := startClockGame(t, `{"turn_timeout_seconds":5,"turn_pace_milliseconds":500}`, services.ServerBot{Manual: true}, services.ServerBot{Manual: true})
	watcher := domain.Principal{PlayerID: uuid.NewString()}
	silent.service.Watchers = lobbyWatchers{watching: map[string]bool{watcher.PlayerID: true}}
	for silent.stored(t).Outcome == nil {
		*silent.clock = silent.turnEnd(t)
		if _, err := silent.service.Watch(context.Background(), watcher, silent.lobbyID); err != nil {
			t.Fatal(err)
		}
		if silent.generationCount(t) > 30 {
			t.Fatal("the game never ended")
		}
	}
	if outcome := silent.stored(t).Outcome; outcome.Reason != models.GameOutcomeAbandoned || len(outcome.Departures) != 0 {
		t.Fatalf("outcome = %+v", outcome)
	}
}

// With the hook alone, a game whose players all closed their tabs ends.
func TestTheRoundEndedHookEndsOnlyAnAbandonedGame(t *testing.T) {
	game := startClockGame(t, `{"turn_timeout_seconds":5}`, services.ServerBot{Manual: true}, services.ServerBot{Manual: true})
	ctx := context.Background()
	generations := game.generationCount(t)
	end := game.turnEnd(t)
	if ended, err := services.EndAbandonedRound(ctx, game.database, game.publisher, game.lobbyID, 1, end.Add(time.Hour)); err != nil || !ended {
		t.Fatalf("round ended = %v, %v", ended, err)
	}
	if game.generationCount(t) != generations || game.stored(t).Outcome.Reason != models.GameOutcomeAbandoned {
		t.Fatal("the hook played a turn or didn't end the game")
	}
	if ended, err := services.EndAbandonedRound(ctx, game.database, game.publisher, game.lobbyID, 1, end.Add(2*time.Hour)); err != nil || !ended || len(game.publisher.GameEndedEvents(t)) != 2 {
		t.Fatalf("again: %v, %v, %d pushes", ended, err, len(game.publisher.GameEndedEvents(t)))
	}
	// A game of bots isn't ended by the hook.
	bots := startClockGame(t, `{"turn_timeout_seconds":5}`, services.ServerBot{}, services.ServerBot{Level: 1})
	if ended, err := services.EndAbandonedRound(ctx, bots.database, bots.publisher, bots.lobbyID, 1, time.Now().Add(time.Hour)); err != nil || ended {
		t.Fatalf("a game of bots: %v, %v", ended, err)
	}
}

// With turn_timeout_seconds null nobody leaves by silence and no game is
// abandoned, after an hour of fake time, and nothing is written.
func TestNoLimitNeverLeavesOrAbandons(t *testing.T) {
	game := startClockGame(t, `{"turn_timeout_seconds":null}`, services.ServerBot{Manual: true}, services.ServerBot{Manual: true})
	writes := countSeenWrites(t, game.database)
	started := game.open(t).CreatedAt
	// An hour in which only Blue calls, once a minute; each call plays one
	// turn, however late it is.
	for minute := 1; minute <= 60; minute++ {
		before := game.generationCount(t)
		*game.clock = started.Add(time.Duration(minute) * time.Minute)
		state := game.applyDeadline(t, game.players[0])
		if state.Outcome != nil || castleOf(state, red) == nil || game.generationCount(t) != before+1 {
			t.Fatalf("after %d minutes: outcome %+v, %d turns played", minute, state.Outcome, game.generationCount(t)-before)
		}
	}
	if ended, err := services.EndAbandonedRound(context.Background(), game.database, nil, game.lobbyID, 1, game.clock.Add(time.Hour)); err != nil || ended {
		t.Fatalf("round ended = %v, %v", ended, err)
	}
	if writes.Load() != 0 {
		t.Fatalf("%d last_seen_at writes without a limit", writes.Load())
	}
}

// A resignation in a game of bots is refused, and changes nothing: its turn
// limit behaves as before.
func TestResignInAGameOfBotsIsRefused(t *testing.T) {
	game := startClockGame(t, `{"turn_timeout_seconds":5}`, services.ServerBot{}, services.ServerBot{Level: 1})
	_, err := game.service.Resign(context.Background(), game.players[0], game.lobbyID)
	assertProblem(t, err, 409, services.NotRealTimeProblemType, "")
	if stored := game.stored(t); len(stored.Departures) != 0 || stored.Outcome != nil {
		t.Fatalf("the refusal changed the game: %+v", stored)
	}
	*game.clock = game.open(t).CreatedAt.Add(5 * time.Second)
	state := game.applyDeadline(t, game.players[0])
	if state.Outcome == nil || state.Outcome.Reason != models.GameOutcomeTurnTimeout || len(state.Outcome.Departures) != 0 {
		t.Fatalf("the turn limit = %+v", state.Outcome)
	}
}

// A localhost bot whose submissions stop is taken out after the turn limit;
// one that answers late every turn, and gets 409 for the stale turn, stays.
// A localhost seat is heard only through its own submissions, not through its
// relayer's turn deliveries.
func TestLocalhostBotsLeaveWhenTheirSubmissionsStop(t *testing.T) {
	ctx := context.Background()
	t.Run("late every turn stays", func(t *testing.T) {
		game := startClockGame(t, `{"turn_timeout_seconds":5}`, services.ServerBot{Manual: true}, services.ServerBot{})
		bluePlayer := game.players[0]
		for turn := range 20 {
			missed := game.current(t, bluePlayer)
			game.toTurnEnd(t, bluePlayer)
			// The bot's answer to the turn it was given comes half a second
			// after that turn ended, with its submit token.
			*game.clock = game.clock.Add(500 * time.Millisecond)
			token, err := game.tokens.Issue(submittoken.Claims{GameID: game.gameID, GenerationID: missed.GenerationID, PlayerID: game.players[1].PlayerID, Expires: time.Now().Add(time.Hour)})
			if err != nil {
				t.Fatal(err)
			}
			claims, err := game.tokens.Verify(token, time.Now())
			if err != nil {
				t.Fatal(err)
			}
			principal, err := game.service.AuthorizeSubmitToken(ctx, claims)
			if err != nil {
				t.Fatal(err)
			}
			_, err = game.service.Register(ctx, principal, models.RegisterGenerationCommands{GameID: game.gameID, GenerationID: missed.GenerationID, RequestID: uuid.NewString(), Commands: []models.PublicGenerationCommand{}})
			assertProblem(t, err, 409, services.StaleGenerationProblemType, "")
			if state := game.current(t, bluePlayer); state.Outcome != nil || castleOf(state, red) == nil {
				t.Fatalf("turn %d: the late bot was taken out: %+v", turn, state.Outcome)
			}
		}
	})
	t.Run("silent leaves", func(t *testing.T) {
		game := startClockGame(t, `{"turn_timeout_seconds":5}`, services.ServerBot{Manual: true}, services.ServerBot{})
		state := game.leaveSilently(t)
		if !strings.HasPrefix(state.Outcome.Summary, "Red left the game: Player 2 didn't respond for 6 s") {
			t.Fatalf("summary = %q", state.Outcome.Summary)
		}
	})
	t.Run("a seat whose relayer reads its turns leaves", func(t *testing.T) {
		game := startClockGame(t, `{"turn_timeout_seconds":5}`, services.ServerBot{Manual: true}, services.ServerBot{})
		relayer, seat := game.players[0], game.players[1]
		if err := game.database.Model(&models.FrozenGamePlayer{}).Where(&models.FrozenGamePlayer{GameID: game.gameID, PlayerID: seat.PlayerID}).Update("bot_relay_player_id", relayer.PlayerID).Error; err != nil {
			t.Fatal(err)
		}
		for game.stored(t).Outcome == nil {
			game.toTurnEnd(t, relayer)
			if _, err := game.service.TurnDelivery(ctx, relayer, game.lobbyID, seat.PlayerID, game.tokens, serverBotSubmitURL); err != nil {
				var rejection *services.GenerationError
				if !errors.As(err, &rejection) || rejection.Status != 409 {
					t.Fatal(err)
				}
			}
			if game.generationCount(t) > 20 {
				t.Fatal("the seat never left")
			}
		}
		if outcome := game.stored(t).Outcome; len(outcome.Departures) != 1 || outcome.Departures[0].Color != red || outcome.Departures[0].Kind != models.DepartureLeft {
			t.Fatalf("outcome = %+v", outcome)
		}
	})
}

// leaveSilently has Blue call each turn's end until Red, who makes no
// request, has left, and returns Blue's state then.
func (game clockGame) leaveSilently(t *testing.T) models.GenerationState {
	t.Helper()
	for range 20 {
		if state := game.toTurnEnd(t, game.players[0]); state.Outcome != nil {
			return state
		}
	}
	t.Fatal("Red never left")
	return models.GenerationState{}
}

// A hosted bot is sent a turn only when one is created, so a turn ended late,
// after nobody played for a while, doesn't count the stall against it.
func TestABotIsNotChargedForAStall(t *testing.T) {
	game := startClockGame(t, ``, services.ServerBot{Manual: true}, services.ServerBot{URL: "https://bot.example.org/turn"})
	first := game.open(t)
	turns := game.runners.take()
	claims, err := game.tokens.Verify(turns[0].event.Delivery.Submit.Token, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	*game.clock = first.CreatedAt.Add(300 * time.Millisecond)
	game.register(t, domain.Principal{PlayerID: claims.PlayerID}, models.RegisterGenerationCommands{GameID: claims.GameID, GenerationID: claims.GenerationID, RequestID: uuid.NewString(), Commands: []models.PublicGenerationCommand{}})
	// The hand player's laptop sleeps, and its deadline call comes just
	// within the turn limit after the turn's end.
	*game.clock = game.turnEnd(t).Add(30*time.Second - 100*time.Millisecond)
	state := game.applyDeadline(t, game.players[0])
	if state.Outcome != nil || castleOf(state, red) == nil || state.TurnNumber != 2 {
		t.Fatalf("the hosted bot was taken out after the stall: %+v", state.Outcome)
	}
}

// countSeenWrites counts the updates of frozen_game_players that change a
// row: the last_seen_at writes.
func countSeenWrites(t *testing.T, database *gorm.DB) *atomic.Int64 {
	t.Helper()
	var writes atomic.Int64
	name := "test:count-seen-writes:" + t.Name()
	if err := database.Callback().Update().After("gorm:update").Register(name, func(statement *gorm.DB) {
		if statement.Statement.Table == "frozen_game_players" && statement.RowsAffected > 0 {
			writes.Add(1)
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Callback().Update().Remove(name) })
	return &writes
}

// last_seen_at is written at most once every two seconds per player, over 20
// requests of every kind in one second, refused batches included, and a batch
// refused for its body still counts as hearing from the player.
func TestLastSeenIsWrittenAtMostOnceEveryFewSeconds(t *testing.T) {
	game := startClockGame(t, ``, services.ServerBot{Manual: true}, services.ServerBot{Manual: true})
	ctx := context.Background()
	player := game.players[0]
	writes := countSeenWrites(t, game.database)
	start := game.open(t).CreatedAt.Add(3 * time.Second)
	state := game.current(t, player)
	requests := []func() error{
		func() error { _, err := game.service.Current(ctx, player, game.lobbyID); return err },
		func() error { _, err := game.service.ApplyDeadline(ctx, player, game.lobbyID); return err },
		func() error { _, err := game.service.Register(ctx, player, batch(state, false)); return err },
		func() error {
			_, err := game.service.TurnDelivery(ctx, player, game.lobbyID, "", game.tokens, serverBotSubmitURL)
			return err
		},
		func() error { _, err := game.service.Replay(ctx, player, game.lobbyID); return err },
	}
	for index := range 20 {
		*game.clock = start.Add(time.Duration(index) * 50 * time.Millisecond)
		_ = requests[index%len(requests)]()
	}
	if writes.Load() != 1 {
		t.Fatalf("%d writes for 20 requests in one second, want 1", writes.Load())
	}
	seen := frozen(t, game, player.PlayerID).LastSeenAt
	if !seen.Equal(start) {
		t.Fatalf("last_seen_at = %v, want the first request's %v", seen, start)
	}
	// Two seconds later, a batch refused for its body writes again.
	*game.clock = start.Add(2 * time.Second)
	refused := batch(state, false)
	refused.Memory = models.BotMemory(`"not an object"`)
	if _, err := game.service.Register(ctx, player, refused); err == nil {
		t.Fatal("a batch with a bad memory was accepted")
	}
	if writes.Load() != 2 || !frozen(t, game, player.PlayerID).LastSeenAt.Equal(*game.clock) {
		t.Fatalf("%d writes, last_seen_at %v", writes.Load(), frozen(t, game, player.PlayerID).LastSeenAt)
	}
}

func frozen(t *testing.T, game clockGame, playerID string) *models.FrozenGamePlayer {
	t.Helper()
	player, err := dal.FindFrozenPlayer(game.database, game.gameID, playerID)
	if err != nil || player == nil {
		t.Fatalf("player %s: %v", playerID, err)
	}
	return player
}
