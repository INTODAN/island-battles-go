// Package islandbattles is the Go SDK for writing Island Battles bots.
//
// A bot is a function, Play: it gets a Turn (the board, its team, the upgrade
// cost table and the memory it returned last turn) and returns a Result (its
// commands, game log lines and new memory). It keeps nothing between turns
// itself; what it needs next turn goes into its memory, which the server
// stores.
//
// A bot is also a program. Its main function calls Main, which gives the
// program the subcommands serve, check, fixture and run:
//
//	func main() {
//		islandbattles.Main(play)
//	}
//
// Client plays one seat straight against the server, as run does. See the
// README for a first bot, and the examples directory for the three default
// bots that the game itself plays.
package islandbattles

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"runtime/debug"
	"strings"
)

// Play is a bot: it plays one turn. ctx is cancelled when the turn's time is
// up: when serve or check stops waiting, and under run at the turn's
// deadline. An error passes the turn: the game client that relays the bot
// shows it, and sends nothing; run stops. Logger(ctx) is the bot's logger.
type Play func(ctx context.Context, turn Turn) (Result, error)

// botFailure is an error the bot returned, or a panic in it.
type botFailure struct {
	err      error
	panicked bool
	// traceback is a panic's stack from where it was raised, innermost
	// first, without the runtime's frames.
	traceback string
}

func (failure botFailure) Error() string { return failure.err.Error() }
func (failure botFailure) Unwrap() error { return failure.err }

// callPlay plays one turn and checks the result with ValidateResult. An
// error is a botFailure when the bot returned it or panicked, and otherwise
// the validation's error.
func callPlay(ctx context.Context, play Play, turn Turn) (result Result, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			stack := debug.Stack()
			result, err = Result{}, botFailure{err: fmt.Errorf("panic: %v\n\n%s", recovered, stack), panicked: true, traceback: panicFrames(string(stack))}
		}
	}()
	result, err = play(ctx, turn)
	if err != nil {
		return Result{}, botFailure{err: err}
	}
	if err := ValidateResult(result); err != nil {
		return Result{}, err
	}
	return result, nil
}

// frameOffset is the program counter offset at the end of a stack line.
var frameOffset = regexp.MustCompile(` \+0x[0-9a-f]+$`)

// panicFrames keeps the frames of a stack that debug.Stack took in a
// deferred recover from the frame that panicked on, without offsets.
func panicFrames(stack string) string {
	lines := strings.Split(strings.TrimRight(stack, "\n"), "\n")
	for index, line := range lines {
		if strings.HasPrefix(line, "panic(") && index+2 <= len(lines) {
			lines = lines[index+2:]
			break
		}
	}
	for index, line := range lines {
		lines[index] = frameOffset.ReplaceAllString(line, "")
	}
	return strings.Join(lines, "\n")
}

// failureMessage is the game log's line for a failed turn: the error, and
// for a panic its traceback, innermost frame first, so that a cut line
// still names the error and where it was raised.
func failureMessage(generationID uint, err error) string {
	message := fmt.Sprintf("the bot failed on generation %d", generationID)
	var failure botFailure
	if errors.As(err, &failure) && failure.panicked {
		summary, _, _ := strings.Cut(failure.err.Error(), "\n\n")
		return message + "\n" + summary + "\nTraceback (most recent call first):\n" + failure.traceback
	}
	return message + "\n" + err.Error()
}
