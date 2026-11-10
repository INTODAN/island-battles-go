package islandbattles

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"
)

// checkTimeout is how long check lets the bot take on one turn. Tests
// shorten it.
var checkTimeout = 2 * time.Second

func check(ctx context.Context, program invocation, arguments []string) int {
	flags := newFlags(program, "check", "Calls the bot once on the fixture turn, or on your own turn file, and fails with a\nmessage if it returns an error, panics, returns an invalid result or memory over\n64 KiB, or takes longer than 2 s.")
	turnFile := flags.String("turn", "", "a BotTurn JSON `file` to play instead of the fixture turn")
	if status, ok := parseFlags(flags, arguments); !ok {
		return status
	}
	fail := func(format string, arguments ...any) int {
		_, _ = fmt.Fprintf(program.stderr, "FAIL: "+format+"\n", arguments...)
		return 1
	}
	turn := FixtureTurn()
	if *turnFile != "" {
		data, err := os.ReadFile(*turnFile)
		if err != nil {
			return fail("could not read the turn: %v", err)
		}
		if turn, err = DecodeTurn(data); err != nil {
			return fail("%s is not a BotTurn: %v", *turnFile, err)
		}
	}
	ctx, cancel := context.WithTimeout(ctx, checkTimeout)
	defer cancel()
	started := time.Now()
	type outcome struct {
		result Result
		err    error
	}
	// The bot runs in its own goroutine, so a bot stuck in a loop can't
	// stop check from failing. The program exits right after.
	done := make(chan outcome, 1)
	go func() {
		result, err := callPlay(ctx, program.play, turn)
		done <- outcome{result, err}
	}()
	var played outcome
	select {
	case played = <-done:
	case <-ctx.Done():
	}
	elapsed := time.Since(started)
	if ctx.Err() != nil {
		if !errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return fail("interrupted")
		}
		return fail("the bot took longer than %g s on one turn", checkTimeout.Seconds())
	}
	var failure botFailure
	switch {
	case errors.As(played.err, &failure) && failure.panicked:
		return fail("the bot panicked: %v", failure.err)
	case errors.As(played.err, &failure):
		return fail("the bot returned an error: %v", failure.err)
	case errors.Is(played.err, ErrMemoryTooLarge):
		return fail("%v", played.err)
	case played.err != nil:
		return fail("the bot returned an invalid result: %v", played.err)
	}
	memorySize, err := played.result.Memory.Size()
	if err != nil {
		return fail("the bot returned an invalid result: %v", err)
	}
	_, _ = fmt.Fprintf(program.stdout, "ok: %d commands, %d log lines, memory %d of %d bytes, %.2f s\n",
		len(played.result.Commands), len(played.result.Logs), memorySize, MemoryLimitBytes, elapsed.Seconds())
	return 0
}
