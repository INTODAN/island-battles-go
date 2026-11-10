// Package islandbattles is the Go SDK for writing Island Battles bots.
//
// A bot is a function, Play: it gets a Turn (the board, its team, the upgrade
// cost table and the memory it returned last turn) and returns a Result (its
// commands, game log lines and new memory). It keeps nothing between turns
// itself; what it needs next turn goes into its memory, which the server
// stores.
//
// A bot is also a program. Its main function calls Main, which gives the
// program the subcommands serve, check and fixture:
//
//	func main() {
//		islandbattles.Main(play)
//	}
//
// See the README for a first bot, and the examples directory for the three
// default bots that the game itself plays.
package islandbattles

import (
	"context"
	"fmt"
	"runtime/debug"
)

// Play is a bot: it plays one turn. ctx is cancelled when the turn's time is
// up, when the caller stops waiting. An error passes the turn: the game
// client that relays the bot shows it, and sends nothing.
type Play func(ctx context.Context, turn Turn) (Result, error)

// botFailure is an error the bot returned, or a panic in it.
type botFailure struct {
	err      error
	panicked bool
}

func (failure botFailure) Error() string { return failure.err.Error() }
func (failure botFailure) Unwrap() error { return failure.err }

// callPlay plays one turn and checks the result with ValidateResult. An
// error is a botFailure when the bot returned it or panicked, and otherwise
// the validation's error.
func callPlay(ctx context.Context, play Play, turn Turn) (result Result, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			result, err = Result{}, botFailure{err: fmt.Errorf("panic: %v\n\n%s", recovered, debug.Stack()), panicked: true}
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
