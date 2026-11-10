package islandbattles

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
)

// logLevels are the --log-level flag's values.
var logLevels = map[string]slog.Level{
	"debug":   slog.LevelDebug,
	"info":    slog.LevelInfo,
	"warning": slog.LevelWarn,
	"error":   slog.LevelError,
}

// runSeat plays one seat of a lobby's game to its end with a Client.
func runSeat(ctx context.Context, program invocation, arguments []string) int {
	flags := newFlags(program, "run", "Plays one seat of a lobby's game to its end, straight against the server, without a\nbrowser. Choose your localhost bot in the lobby before getting ready.")
	baseURL := flags.String("base-url", "", "the game server's `URL`, such as http://localhost:8888 (required)")
	token := flags.String("token", "", "the player's guest bearer `credential` (required)")
	lobbyID := flags.String("lobby-id", "", "the `ID` of the lobby whose game to play (required)")
	logFile := flags.String("log-file", "", "also append the log to this `file`")
	logLevel := flags.String("log-level", "info", "the lowest `level` logged: debug, info, warning or error")
	if status, ok := parseFlags(flags, arguments); !ok {
		return status
	}
	level, found := logLevels[*logLevel]
	if *baseURL == "" || *token == "" || *lobbyID == "" || !found {
		_, _ = fmt.Fprintln(program.stderr, "--base-url, --token and --lobby-id are required, and --log-level is debug, info, warning or error")
		flags.Usage()
		return 2
	}
	options := []ClientOption{
		WithLogLevel(level),
		WithLogOutput(program.stdout),
		WithGameEnded(func(_ context.Context, outcome GameOutcome, generation Generation) {
			_, _ = fmt.Fprintf(program.stdout, "game ended at generation %d: %s\n", generation.ID, outcome.Summary)
		}),
	}
	if *logFile != "" {
		options = append(options, WithLogFile(*logFile))
	}
	options = append(options, program.clientOptions...)
	client, err := NewClient(*baseURL, *token, *lobbyID, program.play, options...)
	if err != nil {
		_, _ = fmt.Fprintf(program.stderr, "%v\n", err)
		return 2
	}
	if err := client.Run(ctx); err != nil {
		if errors.Is(err, context.Canceled) {
			_, _ = fmt.Fprintln(program.stderr, "interrupted; an open turn stays open")
			return 1
		}
		_, _ = fmt.Fprintf(program.stderr, "FAIL: %v\n", err)
		return 1
	}
	return 0
}
