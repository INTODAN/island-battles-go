package islandbattles

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
)

// worker is the process that the server's runners feed turns to, in JSON
// Lines over stdin and stdout (UTF-8):
//
//  1. After starting, the worker writes {"ready": true}.
//  2. For each turn the runner writes one line {"turn": <BotTurn>}. The worker
//     answers one line, {"result": <BotTurnResult>} or {"error": "<message>"}.
//  3. The result is checked like check does (memory size, commands, log
//     lines), and the lines the bot logged come first in its logs.
//  4. The bot's own stdout goes to stderr, so prints never corrupt the
//     protocol. Stderr is free text.
//  5. The worker exits when stdin closes.
func worker(ctx context.Context, program invocation, arguments []string) int {
	flags := newFlags(program, "worker", "Plays turns for the server's runners: JSON Lines on stdin and stdout.\nIt writes {\"ready\": true}, then answers each {\"turn\": ...} line with {\"result\": ...}\nor {\"error\": \"...\"}, and exits when stdin closes. The bot's own output goes to stderr.")
	if status, ok := parseFlags(flags, arguments); !ok {
		return status
	}
	// The protocol keeps the real stdout; the bot's prints, and the default
	// logger, go to stderr.
	protocol := program.stdout
	if file, ok := protocol.(*os.File); ok && file == os.Stdout {
		os.Stdout = os.Stderr
	}
	var requests io.Reader = os.Stdin
	if program.stdin != nil {
		requests = program.stdin
	}
	if err := serveLines(ctx, program.play, requests, protocol, program.stderr); err != nil {
		_, _ = fmt.Fprintf(program.stderr, "the worker stopped: %v\n", err)
		return 1
	}
	return 0
}

// workerAnswer is one line of the worker's output.
type workerAnswer struct {
	Ready  bool            `json:"ready,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *string         `json:"error,omitempty"`
}

func errorAnswer(format string, arguments ...any) workerAnswer {
	message := fmt.Sprintf(format, arguments...)
	return workerAnswer{Error: &message}
}

// serveLines writes the ready line, then answers each request line from
// requests until it ends or ctx is cancelled.
func serveLines(ctx context.Context, play Play, requests io.Reader, protocol, stderr io.Writer) error {
	write := func(answer workerAnswer) error {
		line, err := json.Marshal(answer)
		if err != nil {
			return err
		}
		_, err = protocol.Write(append(line, '\n'))
		return err
	}
	if err := write(workerAnswer{Ready: true}); err != nil {
		return err
	}
	botLog := &logOutputs{level: slog.LevelInfo, writers: []io.Writer{stderr}}
	// A goroutine reads, so that cancelling ctx (SIGTERM) ends the worker
	// even while it waits for the next line.
	type input struct {
		line []byte
		err  error
	}
	inputs := make(chan input)
	go func() {
		reader := bufio.NewReader(requests)
		for {
			line, err := reader.ReadBytes('\n')
			select {
			case inputs <- input{line, err}:
			case <-ctx.Done():
				return
			}
			if err != nil {
				return
			}
		}
	}()
	for {
		select {
		case <-ctx.Done():
			return nil
		case next := <-inputs:
			if line := bytes.TrimSpace(next.line); len(line) > 0 {
				if err := write(answerLine(ctx, play, line, botLog)); err != nil {
					return err
				}
			}
			if errors.Is(next.err, io.EOF) {
				return nil
			}
			if next.err != nil {
				return next.err
			}
		}
	}
}

// answerLine plays the turn in one request line.
func answerLine(ctx context.Context, play Play, line []byte, botLog *logOutputs) workerAnswer {
	var request struct {
		Turn json.RawMessage `json:"turn"`
	}
	if err := json.Unmarshal(line, &request); err != nil {
		return errorAnswer("the line is not a JSON object: %v", err)
	}
	if len(request.Turn) == 0 {
		return errorAnswer(`the line must be an object like {"turn": ...}`)
	}
	turn, err := DecodeTurn(request.Turn)
	if err != nil {
		return errorAnswer("the turn is not a BotTurn:\n%v", err)
	}
	// The lines the bot logs come first in the result's logs, within the game
	// log's limits, as under serve.
	turnLog := newTurnLog(newGameLog(turn.Generation.ID, 0), botLog)
	result, err := callPlay(turnLog.context(ctx), play, turn)
	turnLog.close()
	if err != nil {
		return errorAnswer("%s", err.Error())
	}
	turnLog.addResult(result)
	result.Logs, _ = turnLog.gameLog.batch(true)
	encoded, err := json.Marshal(result)
	if err != nil {
		return errorAnswer("could not encode the result: %v", err)
	}
	return workerAnswer{Result: encoded}
}
