// Command conformance is the Go SDK's adapter for the player client
// conformance suite (clients/conformance): it speaks the suite's JSON Lines
// protocol on stdin and stdout, and logs to stderr.
//
//	ISLAND_BATTLES_CLIENT_COMMAND="go run ./clients/island-battles-go/conformance" \
//		uv run --project clients/conformance --locked pytest clients/conformance/tests/test_models.py
//	./scripts/test-client-conformance.sh "go -C $PWD run ./clients/island-battles-go/conformance"
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"sync"
	"sync/atomic"
	"time"

	islandbattles "github.com/INTODAN/island-battles-go/clients/island-battles-go"
)

// errInvalidArgument is the error of an order whose values a helper can't
// take, such as a missing castle ID or a soldier amount of 1.5. Go's
// helpers take typed values, so the adapter checks the values as given.
var errInvalidArgument = errors.New("invalid argument")

// logLevels map the protocol's levels onto slog's.
var logLevels = map[string]slog.Level{
	"debug":   slog.LevelDebug,
	"info":    slog.LevelInfo,
	"warning": slog.LevelWarn,
	"error":   slog.LevelError,
}

// message is any message from the runner.
type message struct {
	Type       string            `json:"type"`
	ID         json.RawMessage   `json:"id"`
	Generation json.RawMessage   `json:"generation"`
	GameURL    string            `json:"game_url"`
	LobbyURL   string            `json:"lobby_url"`
	Credential string            `json:"credential"`
	LobbyID    string            `json:"lobby_id"`
	LogFile    string            `json:"log_file"`
	Level      string            `json:"level"`
	Message    string            `json:"message"`
	Commands   []json.RawMessage `json:"commands"`
	Logs       []struct {
		Level   string `json:"level"`
		Message string `json:"message"`
	} `json:"logs"`
	Memory json.RawMessage `json:"memory"`
}

// adapter translates protocol messages into SDK calls.
type adapter struct {
	outputMutex sync.Mutex
	output      io.Writer
	// decisions are the runner's log, finish and fail messages, which the
	// bot acts on during the current turn, or the next one.
	decisions chan message
	closing   atomic.Bool
	clientMu  sync.Mutex
	client    *islandbattles.Client
}

func main() {
	adapter := &adapter{output: os.Stdout, decisions: make(chan message, 4096)}
	adapter.serve(os.Stdin)
}

// serve processes the input until close or its end, then closes.
func (adapter *adapter) serve(input io.Reader) {
	reader := bufio.NewReaderSize(input, 1<<20)
	for {
		line, err := reader.ReadBytes('\n')
		if len(line) > 0 {
			var received message
			if decodeError := json.Unmarshal(line, &received); decodeError != nil {
				_, _ = fmt.Fprintf(os.Stderr, "unreadable message: %v\n", decodeError)
			} else if adapter.handle(received) {
				break
			}
		}
		if err != nil {
			break
		}
	}
	adapter.close()
}

// handle acts on one message, and returns true for close.
func (adapter *adapter) handle(received message) bool {
	switch received.Type {
	case "evaluate":
		adapter.evaluate(received)
	case "start":
		go adapter.run(received)
	case "finish", "fail", "log":
		adapter.decisions <- received
	case "close":
		return true
	}
	return false
}

func (adapter *adapter) emit(value map[string]any) {
	encoded, err := json.Marshal(value)
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "could not encode %v: %v\n", value["type"], err)
		return
	}
	adapter.outputMutex.Lock()
	defer adapter.outputMutex.Unlock()
	_, _ = adapter.output.Write(append(encoded, '\n'))
}

// measured is a castle or a soldier group, with the utilities.
type measured interface {
	islandbattles.Entity
	Distance(other islandbattles.Entity) float64
	CompareTeam(other islandbattles.Entity) islandbattles.TeamType
}

// evaluate parses a wire generation and reports the public model's
// utilities.
func (adapter *adapter) evaluate(received message) {
	generation, err := islandbattles.DecodeGeneration(received.Generation)
	if err != nil {
		adapter.emit(map[string]any{"type": "evaluation", "id": received.ID, "ok": false})
		return
	}
	entities := []measured{}
	for _, castle := range generation.Castles {
		entities = append(entities, castle)
	}
	for _, group := range generation.SoldierGroups {
		entities = append(entities, group)
	}
	distances := make([][]float64, len(entities))
	relations := make([][]islandbattles.TeamType, len(entities))
	for index, entity := range entities {
		distances[index] = []float64{}
		relations[index] = []islandbattles.TeamType{}
		for _, other := range entities {
			distances[index] = append(distances[index], entity.Distance(other))
			relations[index] = append(relations[index], entity.CompareTeam(other))
		}
	}
	distanceLeft := []float64{}
	for _, group := range generation.SoldierGroups {
		left, _ := group.DistanceLeft(generation)
		distanceLeft = append(distanceLeft, left)
	}
	adapter.emit(map[string]any{
		"type": "evaluation", "id": received.ID, "ok": true, "generation": generation,
		"distances": distances, "relations": relations, "distance_left": distanceLeft,
	})
}

// run runs the client until it is closed or fails.
func (adapter *adapter) run(received message) {
	options := []islandbattles.ClientOption{
		islandbattles.WithLogLevel(slog.LevelDebug),
		// Stdout carries the protocol; the log goes to stderr instead.
		islandbattles.WithLogOutput(os.Stderr),
		islandbattles.WithGameEnded(func(_ context.Context, outcome islandbattles.GameOutcome, generation islandbattles.Generation) {
			adapter.emit(map[string]any{"type": "ended", "outcome": outcome, "generation": generation})
		}),
	}
	if received.LogFile != "" {
		options = append(options, islandbattles.WithLogFile(received.LogFile))
	}
	client, err := islandbattles.NewClient(islandbattles.URLs{Game: received.GameURL, Lobby: received.LobbyURL}, received.Credential, received.LobbyID, adapter.play, options...)
	if err != nil {
		adapter.emit(map[string]any{"type": "stopped", "error": describe(err)})
		return
	}
	adapter.clientMu.Lock()
	adapter.client = client
	adapter.clientMu.Unlock()
	if adapter.closing.Load() {
		return
	}
	_, err = client.Start(context.Background())
	if err == nil {
		adapter.emit(map[string]any{"type": "started", "team_color": client.TeamColor()})
		err = client.Wait(context.Background())
	}
	if err != nil {
		if !adapter.closing.Load() {
			adapter.emit(map[string]any{"type": "stopped", "error": describe(err)})
		}
		_ = client.Close(context.Background())
	}
}

// play reports the turn, then acts on the runner's orders: log, then finish
// or fail.
func (adapter *adapter) play(ctx context.Context, turn islandbattles.Turn) (islandbattles.Result, error) {
	var deadline any
	if turn.TurnDeadlineAt != nil {
		deadline = turn.TurnDeadlineAt.Format(time.RFC3339Nano)
	}
	adapter.emit(map[string]any{
		"type": "generation", "generation": turn.Generation, "memory": turn.Memory,
		"team_color": turn.TeamColor, "game_id": turn.GameID, "upgrades": turn.Upgrades, "turn_deadline_at": deadline,
	})
	logger := islandbattles.Logger(ctx)
	done := ctx.Done()
	for {
		var decision message
		select {
		case decision = <-adapter.decisions:
		case <-done:
			if errors.Is(ctx.Err(), context.Canceled) {
				return islandbattles.Result{}, ctx.Err()
			}
			// The turn's deadline passed; the runner still decides.
			done = nil
			continue
		}
		switch decision.Type {
		case "log":
			// Logged inside the turn, so the line goes to the game log.
			logger.Log(ctx, logLevels[decision.Level], decision.Message)
			adapter.emit(map[string]any{"type": "logged"})
		case "fail":
			// A panic is Go's raise: it carries the traceback.
			panic(errors.New("scripted callback failure"))
		default:
			return finish(decision)
		}
	}
}

// finish builds the result of a finish message, with the values as given.
func finish(decision message) (islandbattles.Result, error) {
	var result islandbattles.Result
	for _, raw := range decision.Commands {
		command, err := order(raw)
		if err != nil {
			return islandbattles.Result{}, err
		}
		result.Commands = append(result.Commands, command)
	}
	for _, line := range decision.Logs {
		result.Logs = append(result.Logs, islandbattles.LogAt(islandbattles.LogLevel(line.Level), line.Message))
	}
	if len(decision.Memory) > 0 {
		result.Memory = islandbattles.Memory(decision.Memory)
	}
	return result, nil
}

// order builds a command with the SDK's helpers from the runner's order.
func order(raw json.RawMessage) (islandbattles.Command, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return islandbattles.Command{}, fmt.Errorf("%w: %v", errInvalidArgument, err)
	}
	var orderType string
	_ = json.Unmarshal(fields["type"], &orderType)
	if orderType == "send_soldier_group" {
		var origin, destination, amount uint
		if err := errors.Join(field(fields, "origin_castle_id", &origin), field(fields, "destination_castle_id", &destination), field(fields, "soldier_amount", &amount)); err != nil {
			return islandbattles.Command{}, err
		}
		return islandbattles.SendSoldierGroup(origin, destination, amount), nil
	}
	var castleID uint
	var upgradeType string
	if err := errors.Join(field(fields, "castle_id", &castleID), field(fields, "upgrade_type", &upgradeType)); err != nil {
		return islandbattles.Command{}, err
	}
	return islandbattles.UpgradeCastle(castleID, islandbattles.UpgradeType(upgradeType)), nil
}

// field decodes one of an order's values into the helper argument's type.
func field(fields map[string]json.RawMessage, name string, target any) error {
	raw, found := fields[name]
	if !found {
		return fmt.Errorf("%w: %s is missing", errInvalidArgument, name)
	}
	if err := json.Unmarshal(raw, target); err != nil {
		return fmt.Errorf("%w: %s: %v", errInvalidArgument, name, err)
	}
	return nil
}

// describe maps an SDK failure onto the protocol's error kinds.
func describe(err error) map[string]any {
	var status any
	var gameError *islandbattles.GameError
	if errors.As(err, &gameError) && gameError.Status != 0 {
		status = gameError.Status
	}
	kind := "player"
	for _, candidate := range []struct {
		target error
		kind   string
	}{
		{errInvalidArgument, "invalid_argument"},
		{islandbattles.ErrInvalidResult, "invalid_argument"},
		{islandbattles.ErrMemoryTooLarge, "memory_too_large"},
		{islandbattles.ErrClientClosed, "closed"},
		{islandbattles.ErrAuthentication, "authentication"},
		{islandbattles.ErrTransport, "transport"},
		{islandbattles.ErrProtocol, "protocol"},
		{islandbattles.ErrCastleNotFound, "castle_not_found"},
		{islandbattles.ErrCastleNotOwned, "not_owned"},
		{islandbattles.ErrUpgradeMaxLevel, "max_level"},
		{islandbattles.ErrInsufficientSoldiers, "insufficient_soldiers"},
	} {
		if errors.Is(err, candidate.target) {
			kind = candidate.kind
			break
		}
	}
	if kind == "player" && gameError != nil {
		kind = "rejected"
	}
	return map[string]any{"kind": kind, "status": status, "detail": err.Error()}
}

// close closes the client without completing an open turn.
func (adapter *adapter) close() {
	adapter.closing.Store(true)
	adapter.clientMu.Lock()
	client := adapter.client
	adapter.clientMu.Unlock()
	if client != nil {
		_ = client.Close(context.Background())
	}
	adapter.emit(map[string]any{"type": "closed"})
}
