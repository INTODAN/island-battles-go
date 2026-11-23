package services_test

import (
	"testing"
	"time"

	"islandBattles/backend/models"
	"islandBattles/backend/services"
)

// Each real-time turn ends one pace after the previous turn's end, not after
// the late request that played it, so turns come one pace apart whatever the
// latency.
func TestRealTimeTurnsComeOnePaceApartWhateverTheLatency(t *testing.T) {
	game := startClockGame(t, ``, services.ServerBot{Manual: true}, services.ServerBot{Level: 2})
	manual := game.players[0]
	pace := time.Duration(models.DefaultTurnPaceMilliseconds) * time.Millisecond
	// The first turn ends one pace after the start.
	firstEnd := game.open(t).CreatedAt.Add(pace)
	if end := game.turnEnd(t); !end.Equal(firstEnd) {
		t.Fatalf("the first turn ends at %v, want %v", end, firstEnd)
	}
	for turn := range 10 {
		// Each deadline call reaches the server 300 ms after the turn's end.
		*game.clock = game.turnEnd(t).Add(300 * time.Millisecond)
		state := game.applyDeadline(t, manual)
		want := firstEnd.Add(time.Duration(turn+1) * pace)
		if state.TurnNumber != turn+2 || state.TurnDeadlineAt == nil || !state.TurnDeadlineAt.Equal(want) {
			t.Fatalf("turn %d ends at %v, want %v", state.TurnNumber, state.TurnDeadlineAt, want)
		}
		if bot := game.current(t, game.players[1]); bot.TurnDeadlineAt == nil || !bot.TurnDeadlineAt.Equal(want) {
			t.Fatalf("the bot's turn ends at %v, want %v", bot.TurnDeadlineAt, want)
		}
	}
}

// A turn that plays very late, but within the turn limit, gives the next turn
// half a pace, so the next request doesn't play it at once, and the turns go
// on one pace apart from there.
func TestATurnPlayedVeryLateGivesTheNextHalfAPace(t *testing.T) {
	game := startClockGame(t, ``, services.ServerBot{Manual: true}, services.ServerBot{Level: 2})
	manual := game.players[0]
	pace := time.Duration(models.DefaultTurnPaceMilliseconds) * time.Millisecond
	late := game.turnEnd(t).Add(5 * time.Second)
	*game.clock = late
	state := game.applyDeadline(t, manual)
	if state.TurnNumber != 2 || state.TurnDeadlineAt == nil || !state.TurnDeadlineAt.Equal(late.Add(pace/2)) {
		t.Fatalf("after a late turn: turn %d ends at %v, want %v", state.TurnNumber, state.TurnDeadlineAt, late.Add(pace/2))
	}
	// Requests until then play nothing: no cascade of turns.
	for _, after := range []time.Duration{0, 100 * time.Millisecond, pace/2 - time.Millisecond} {
		*game.clock = late.Add(after)
		if again := game.applyDeadline(t, manual); again.TurnNumber != 2 {
			t.Fatalf("a request %s after the late turn played turn %d", after, again.TurnNumber)
		}
	}
	*game.clock = late.Add(pace / 2)
	if next := game.applyDeadline(t, manual); next.TurnNumber != 3 || next.TurnDeadlineAt == nil || !next.TurnDeadlineAt.Equal(late.Add(pace/2+pace)) {
		t.Fatalf("the turn after: %d ending at %v", next.TurnNumber, next.TurnDeadlineAt)
	}
}

func TestNextTurnDeadline(t *testing.T) {
	pace := uint(1000)
	game := &models.Game{TurnPaceMilliseconds: &pace}
	start := time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC)
	deadline := start.Add(3 * time.Second)
	later := &models.Generation{TurnDeadlineAt: &deadline}
	first := &models.Generation{}
	first.CreatedAt = start
	cases := []struct {
		name string
		open *models.Generation
		now  time.Time
		want time.Time
	}{
		{"the first turn played on time", first, start.Add(time.Second), start.Add(2 * time.Second)},
		{"the first turn played late", first, start.Add(1300 * time.Millisecond), start.Add(2 * time.Second)},
		{"on time", later, deadline, deadline.Add(time.Second)},
		{"late", later, deadline.Add(300 * time.Millisecond), deadline.Add(time.Second)},
		{"late by half a pace", later, deadline.Add(500 * time.Millisecond), deadline.Add(time.Second)},
		{"too late for half a pace", later, deadline.Add(800 * time.Millisecond), deadline.Add(1300 * time.Millisecond)},
		{"several paces late", later, deadline.Add(5 * time.Second), deadline.Add(5500 * time.Millisecond)},
		{"early, once everyone completed", later, deadline.Add(-700 * time.Millisecond), deadline.Add(300 * time.Millisecond)},
	}
	for _, test := range cases {
		got := services.NextTurnDeadline(game, test.open, test.now)
		if got == nil || !got.Equal(test.want) {
			t.Errorf("%s: %v, want %v", test.name, got, test.want)
		}
	}
	timeout := uint(30)
	if got := services.NextTurnDeadline(&models.Game{TurnTimeoutSeconds: &timeout}, first, start); got != nil {
		t.Errorf("a game that waits got %v", got)
	}
}
