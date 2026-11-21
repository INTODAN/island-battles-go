package services

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"islandBattles/backend/bots"
	"islandBattles/backend/models"
)

// headlessPlayerFunc is a HeadlessPlayer from a function.
type headlessPlayerFunc func(ctx context.Context, turn models.BotTurn) (models.BotTurnResult, error)

func (play headlessPlayerFunc) Play(ctx context.Context, turn models.BotTurn) (models.BotTurnResult, error) {
	return play(ctx, turn)
}

func defaultHeadlessSeat(t *testing.T, level uint) HeadlessSeat {
	t.Helper()
	bot, found := bots.ForLevel(level)
	if !found {
		t.Fatalf("there is no default bot of level %d", level)
	}
	return HeadlessSeat{Name: "level " + string(rune('0'+level)), Player: bot}
}

func straitMap(t *testing.T) models.Map {
	t.Helper()
	for _, candidate := range tournamentMaps() {
		if candidate.name == "strait" {
			return candidate.gameMap
		}
	}
	t.Fatal("there is no strait map")
	return models.Map{}
}

// passes is a player that sends nothing.
var passes = headlessPlayerFunc(func(context.Context, models.BotTurn) (models.BotTurnResult, error) {
	return models.BotTurnResult{Commands: []models.PublicGenerationCommand{}, Logs: []models.GenerationLogLine{}}, nil
})

func TestHeadlessMatchPlaysDefaultBotsToAnOutcome(t *testing.T) {
	match := HeadlessMatch{Map: straitMap(t), Seats: []HeadlessSeat{defaultHeadlessSeat(t, 3), defaultHeadlessSeat(t, 1)}}
	result, err := PlayHeadlessMatch(context.Background(), match)
	if err != nil {
		t.Fatal(err)
	}
	if result.Outcome.WinnerTeamColor == nil || *result.Outcome.WinnerTeamColor != 1 || result.Outcome.Summary != "level 3 wins as the last team standing" {
		t.Fatalf("outcome = %+v, want level 3, team 1, to win", result.Outcome)
	}
	if result.Generations != len(result.Demo.Frames) || len(result.Demo.Logs) != len(result.Demo.Frames) {
		t.Fatalf("%d generations, %d frames and %d log entries, want them equal", result.Generations, len(result.Demo.Frames), len(result.Demo.Logs))
	}
	winner, loser := result.Seats[0], result.Seats[1]
	if winner.FailedTurns != 0 || loser.FailedTurns != 0 {
		t.Fatalf("failed turns = %d and %d, want none: %+v %+v", winner.FailedTurns, loser.FailedTurns, winner.Failures, loser.Failures)
	}
	if winner.Castles == 0 || winner.EliminatedAtTurn != 0 || loser.Castles != 0 || loser.Soldiers != 0 || loser.EliminatedAtTurn != result.Generations {
		t.Fatalf("seats = %+v and %+v, want the loser eliminated in the last generation", winner, loser)
	}
	if winner.TurnsPlayed != result.Generations-1 || winner.Commands == 0 {
		t.Fatalf("the winner played %d turns with %d commands in %d generations", winner.TurnsPlayed, winner.Commands, result.Generations)
	}
	// The demo is what Save demo writes: the outcome, and every team's lines.
	if result.Demo.Frames[0].ID != headlessFirstGenerationID || result.Demo.Logs[1].GenerationID != headlessFirstGenerationID+1 {
		t.Fatalf("the first frame's ID is %d and the second log entry's %d, want IDs from %d", result.Demo.Frames[0].ID, result.Demo.Logs[1].GenerationID, headlessFirstGenerationID)
	}
	if result.Demo.Outcome == nil || result.Demo.Format != models.DemoFormat || len(result.Demo.Logs[0].Players) != 2 || len(result.Demo.Logs[0].HiddenTeamColors) != 0 {
		t.Fatalf("demo = %+v, want an outcome and both teams' first lines", result.Demo.Logs[0])
	}
	replayed, err := PlayHeadlessMatch(context.Background(), match)
	if err != nil {
		t.Fatal(err)
	}
	first, _ := json.Marshal(result.Demo.Frames)
	second, _ := json.Marshal(replayed.Demo.Frames)
	if string(first) != string(second) {
		t.Fatal("the same match played twice gave different games")
	}
}

func TestHeadlessMatchEndsAtTheGenerationLimit(t *testing.T) {
	result, err := PlayHeadlessMatch(context.Background(), HeadlessMatch{Map: straitMap(t), MaxGenerations: 5,
		Seats: []HeadlessSeat{{Name: "first", Player: passes}, {Name: "second", Player: passes}}})
	if err != nil {
		t.Fatal(err)
	}
	if result.Generations != 5 || result.Outcome.Reason != models.GameOutcomeGenerationCap || result.Outcome.WinnerTeamColor != nil {
		t.Fatalf("%d generations and outcome %+v, want a draw at generation 5", result.Generations, result.Outcome)
	}
	if result.Seats[0].TurnsPlayed != 4 {
		t.Fatalf("turns played = %d, want 4", result.Seats[0].TurnsPlayed)
	}
}

func TestHeadlessPlayerThatFailsPassesItsTurn(t *testing.T) {
	notOwned, err := json.Marshal(map[string]uint{"origin_castle_id": 1, "destination_castle_id": 0, "soldier_amount": 1})
	if err != nil {
		t.Fatal(err)
	}
	players := map[string]HeadlessPlayer{
		"an error": headlessPlayerFunc(func(context.Context, models.BotTurn) (models.BotTurnResult, error) {
			return models.BotTurnResult{}, errors.New("no plan")
		}),
		"a panic": headlessPlayerFunc(func(context.Context, models.BotTurn) (models.BotTurnResult, error) { panic("lost") }),
		"a command the rules reject": headlessPlayerFunc(func(context.Context, models.BotTurn) (models.BotTurnResult, error) {
			return models.BotTurnResult{Commands: []models.PublicGenerationCommand{{CommandType: models.SendSoldierGroup, Parameters: notOwned}}}, nil
		}),
		"an answer after the turn's time": headlessPlayerFunc(func(ctx context.Context, _ models.BotTurn) (models.BotTurnResult, error) {
			<-ctx.Done()
			return models.BotTurnResult{}, ctx.Err()
		}),
	}
	for name, player := range players {
		t.Run(name, func(t *testing.T) {
			result, err := PlayHeadlessMatch(context.Background(), HeadlessMatch{Map: straitMap(t), MaxGenerations: 8, TurnTimeout: 20 * time.Millisecond,
				Seats: []HeadlessSeat{{Name: "failing", Player: player}, {Name: "passing", Player: passes}}})
			if err != nil {
				t.Fatal(err)
			}
			failing := result.Seats[0]
			if failing.FailedTurns != 7 || failing.TurnsPlayed != 7 || len(failing.Failures) != MaxHeadlessFailures || failing.Failures[0].Turn != 1 || failing.Commands != 0 {
				t.Fatalf("seat = %+v, want 7 failed turns and the first %d listed", failing, MaxHeadlessFailures)
			}
			if result.Seats[1].FailedTurns != 0 {
				t.Fatalf("the passing seat failed %d turns", result.Seats[1].FailedTurns)
			}
			lines := result.Demo.Logs[0].Players[0].Lines
			if len(lines) != 1 || lines[0].Level != models.GenerationLogError || !strings.HasPrefix(lines[0].Message, "failing failed this turn, so it passes: ") {
				t.Fatalf("lines = %+v, want one error line that says the seat passes", lines)
			}
		})
	}
}

// A player that doesn't return when its turn's time is up is waited for, so
// that its next turn never runs beside the one that failed; the turn's time
// is the timeout, not the wait.
func TestHeadlessMatchWaitsForALatePlayerBeforeItsNextTurn(t *testing.T) {
	var playing, overlapped atomic.Int32
	deaf := headlessPlayerFunc(func(context.Context, models.BotTurn) (models.BotTurnResult, error) {
		if playing.Add(1) > 1 {
			overlapped.Add(1)
		}
		defer playing.Add(-1)
		time.Sleep(200 * time.Millisecond)
		return models.BotTurnResult{Commands: []models.PublicGenerationCommand{}}, nil
	})
	result, err := PlayHeadlessMatch(context.Background(), HeadlessMatch{Map: straitMap(t), MaxGenerations: 4, TurnTimeout: 20 * time.Millisecond,
		Seats: []HeadlessSeat{{Name: "deaf", Player: deaf}, {Name: "passing", Player: passes}}})
	if err != nil {
		t.Fatal(err)
	}
	if overlapped.Load() != 0 {
		t.Fatalf("%d turns of one seat ran beside another", overlapped.Load())
	}
	seat := result.Seats[0]
	if seat.FailedTurns != 3 || seat.Failures[0].Message != "the bot didn't answer within 20ms" || seat.SlowestTurnMilliseconds < 20 || seat.SlowestTurnMilliseconds > 150 {
		t.Fatalf("seat = %+v, want 3 failed turns of about the 20 ms timeout each", seat)
	}
}

// An eliminated team's seat stays in the game log of every later generation,
// with no lines, as every roster player does in a lobby's demo.
func TestHeadlessDemoListsEverySeatInEveryGeneration(t *testing.T) {
	result, err := PlayHeadlessMatch(context.Background(), HeadlessMatch{Map: straitMap(t), Seats: []HeadlessSeat{defaultHeadlessSeat(t, 3), defaultHeadlessSeat(t, 1)}})
	if err != nil {
		t.Fatal(err)
	}
	for index, logs := range result.Demo.Logs {
		if len(logs.Players) != 2 || logs.Players[0].TeamColor != 1 || logs.Players[1].TeamColor != 2 || logs.Players[0].Lines == nil {
			t.Fatalf("generation %d: players = %+v, want both seats", index+1, logs.Players)
		}
	}
	if last := result.Demo.Logs[len(result.Demo.Logs)-1]; len(last.Players[0].Lines)+len(last.Players[1].Lines) != 0 {
		t.Fatalf("the final generation has lines: %+v", last)
	}
}

func TestHeadlessMatchNeedsASeatForEveryTeam(t *testing.T) {
	for name, seats := range map[string][]HeadlessSeat{
		"one seat":    {{Name: "alone", Player: passes}},
		"three seats": {{Name: "first", Player: passes}, {Name: "second", Player: passes}, {Name: "third", Player: passes}},
	} {
		if _, err := PlayHeadlessMatch(context.Background(), HeadlessMatch{Map: straitMap(t), Seats: seats}); err == nil {
			t.Errorf("%s on a map for two teams: no error", name)
		}
	}
}

func TestHeadlessMatchStopsWhenItsContextIsDone(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	turns := 0
	stopping := headlessPlayerFunc(func(ctx context.Context, turn models.BotTurn) (models.BotTurnResult, error) {
		if turns++; turns == 3 {
			cancel()
		}
		return passes(ctx, turn)
	})
	if _, err := PlayHeadlessMatch(ctx, HeadlessMatch{Map: straitMap(t), Seats: []HeadlessSeat{{Name: "first", Player: stopping}, {Name: "second", Player: passes}}}); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}
