// Command headless-match plays whole games between bots in memory, without a
// database, a lobby, Centrifugo or HTTP (services.PlayHeadlessMatch), for
// playing many games quickly: a bot league, a tournament, a regression check.
//
//	go run ./backend/cmd/headless-match schedule.json > results.jsonl
//
// It reads a schedule, a JSON document, from the file it is given, or from
// stdin without one, plays every match of it, several at a time, and writes
// one JSON line per match to stdout as each one ends: the outcome, each
// seat's numbers and where the demo is. The schedule:
//
//	{
//	  "players": {
//	    "level3": {"default_level": 3},
//	    "mybot": {"worker": ["python", "-m", "island_battles_client.worker", "bot:play"], "directory": "path/to/the/bot"},
//	    "gobot": {"worker": ["path/to/gobot", "worker"]}
//	  },
//	  "maps": {"crossing": "backend/cmd/headless-match/maps/crossing.json"},
//	  "matches": [{"id": "crossing-1", "map": "crossing", "seats": ["mybot", "level3"]}],
//	  "max_generations": 300,
//	  "turn_timeout_milliseconds": 2000,
//	  "parallel": 8,
//	  "demo_directory": "demos",
//	  "compress_demos": true
//	}
//
// A player is a default bot (default_level) or a worker: a command that
// speaks the SDKs' worker protocol, JSON Lines on stdin and stdout, such as
// `python -m island_battles_client.worker module:function` or a Go bot's
// `worker` command. Bots are stateless, so one worker process plays any
// number of games: a match borrows a process for each worker seat, and gives
// it back for the next match, so no more processes run than worker seats
// play at once. A map is a map file.
// seats[index] plays team colour index+1. The README's "Headless matches"
// has the rest.
package main

import (
	"bufio"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"slices"
	"sync"
	"syscall"
	"time"

	"islandBattles/backend/bots"
	"islandBattles/backend/models"
	"islandBattles/backend/services"
)

// schedule is the document the command reads.
type schedule struct {
	Players map[string]playerSpecification `json:"players"`
	// Maps names each map file, by path.
	Maps    map[string]string    `json:"maps"`
	Matches []matchSpecification `json:"matches"`
	// MaxGenerations is every game's last generation; 0 means the default.
	MaxGenerations int `json:"max_generations"`
	// TurnTimeoutMilliseconds is how long a player may take for a turn before
	// it passes; 0 means no limit.
	TurnTimeoutMilliseconds int `json:"turn_timeout_milliseconds"`
	// Parallel is how many matches play at once; 0 means the number of CPUs.
	Parallel int `json:"parallel"`
	// DemoDirectory is where each match's demo is written, as <id>.json, or
	// <id>.json.gz with CompressDemos. Empty writes no demos.
	DemoDirectory string `json:"demo_directory"`
	CompressDemos bool   `json:"compress_demos"`
}

// playerSpecification is a default bot, by level, or a worker, by command.
type playerSpecification struct {
	DefaultLevel *uint    `json:"default_level"`
	Worker       []string `json:"worker"`
	// Directory is the worker's working directory, and Environment is added
	// to this process's environment for it.
	Directory   string            `json:"directory"`
	Environment map[string]string `json:"environment"`
}

type matchSpecification struct {
	ID    string   `json:"id"`
	Map   string   `json:"map"`
	Seats []string `json:"seats"`
}

// matchLine is one line of the output. Error is set, alone with the ID, the
// map and the seats, for a match that couldn't be played.
type matchLine struct {
	ID          string                        `json:"id"`
	Map         string                        `json:"map"`
	Players     []string                      `json:"players"`
	Error       string                        `json:"error,omitempty"`
	Outcome     *models.GameOutcomeView       `json:"outcome,omitempty"`
	Winner      *string                       `json:"winner"`
	Generations int                           `json:"generations,omitempty"`
	Seats       []services.HeadlessSeatResult `json:"seats,omitempty"`
	Demo        string                        `json:"demo,omitempty"`
	Seconds     float64                       `json:"seconds"`
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:], os.Stdin, os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "headless-match:", err)
		os.Exit(2)
	}
}

func run(ctx context.Context, arguments []string, input io.Reader, output, workerErrors io.Writer) error {
	if len(arguments) > 1 {
		return errors.New("usage: headless-match [schedule.json] (the schedule is read from stdin without a file)")
	}
	if len(arguments) == 1 {
		file, err := os.Open(arguments[0])
		if err != nil {
			return err
		}
		defer func() { _ = file.Close() }()
		input = file
	}
	var plan schedule
	decoder := json.NewDecoder(input)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&plan); err != nil {
		return fmt.Errorf("read the schedule: %w", err)
	}
	if plan.Parallel <= 0 {
		plan.Parallel = runtime.NumCPU()
	}
	gameMaps := map[string]models.Map{}
	for name, path := range plan.Maps {
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("map %q: %w", name, err)
		}
		template, err := models.DecodeMapTemplate(data)
		if err != nil {
			return fmt.Errorf("map %q (%s): %w", name, path, err)
		}
		gameMaps[name] = models.BuildMap(template)
	}
	players := map[string]playerSource{}
	var pools []*workerPool
	defer func() {
		for _, pool := range pools {
			pool.close()
		}
	}()
	for name, specification := range plan.Players {
		switch {
		case specification.DefaultLevel != nil && len(specification.Worker) == 0:
			bot, found := bots.ForLevel(*specification.DefaultLevel)
			if !found {
				return fmt.Errorf("player %q: there is no default bot of level %d", name, *specification.DefaultLevel)
			}
			players[name] = defaultBot{bot: bot}
		case specification.DefaultLevel == nil && len(specification.Worker) > 0:
			pool := &workerPool{name: name, specification: specification, workerErrors: workerErrors}
			pools = append(pools, pool)
			players[name] = pool
		default:
			return fmt.Errorf("player %q needs either default_level or worker", name)
		}
	}
	seenIDs := map[string]bool{}
	for index, match := range plan.Matches {
		if match.ID == "" || match.ID != filepath.Base(match.ID) || seenIDs[match.ID] {
			return fmt.Errorf("matches[%d].id %q must be a file name that no other match has", index, match.ID)
		}
		seenIDs[match.ID] = true
		if _, found := gameMaps[match.Map]; !found {
			return fmt.Errorf("match %q: there is no map %q", match.ID, match.Map)
		}
		for _, seat := range match.Seats {
			if _, found := players[seat]; !found {
				return fmt.Errorf("match %q: there is no player %q", match.ID, seat)
			}
		}
	}
	if plan.DemoDirectory != "" {
		if err := os.MkdirAll(plan.DemoDirectory, 0o755); err != nil {
			return err
		}
	}
	var outputLock sync.Mutex
	encoder := json.NewEncoder(output)
	encoder.SetEscapeHTML(false)
	var writeError error
	var waiting sync.WaitGroup
	running := make(chan struct{}, plan.Parallel)
matches:
	for index, match := range plan.Matches {
		// No match starts once the command is told to stop.
		select {
		case <-ctx.Done():
			break matches
		case running <- struct{}{}:
		}
		if ctx.Err() != nil {
			<-running
			break
		}
		waiting.Add(1)
		go func() {
			defer func() { <-running; waiting.Done() }()
			line := playMatch(ctx, plan, uint(index+1), match, gameMaps[match.Map], players)
			outputLock.Lock()
			defer outputLock.Unlock()
			if err := encoder.Encode(line); err != nil && writeError == nil {
				writeError = err
			}
		}()
	}
	waiting.Wait()
	if writeError != nil {
		return writeError
	}
	return ctx.Err()
}

// playMatch plays one match of the schedule and writes its demo.
func playMatch(ctx context.Context, plan schedule, gameID uint, match matchSpecification, gameMap models.Map, players map[string]playerSource) matchLine {
	line := matchLine{ID: match.ID, Map: match.Map, Players: match.Seats}
	seats := make([]services.HeadlessSeat, len(match.Seats))
	for index, name := range match.Seats {
		// Getting a player ready, such as starting a worker's process, is not
		// part of the match: the game's first turn doesn't pay for it.
		player, release, err := players[name].seat(ctx)
		if err != nil {
			line.Error = err.Error()
			return line
		}
		defer release()
		seats[index] = services.HeadlessSeat{Name: name, Player: player}
	}
	started := time.Now()
	result, err := services.PlayHeadlessMatch(ctx, services.HeadlessMatch{GameID: gameID, Map: gameMap, Seats: seats, MaxGenerations: plan.MaxGenerations,
		TurnTimeout: time.Duration(plan.TurnTimeoutMilliseconds) * time.Millisecond})
	if err == nil && plan.DemoDirectory != "" {
		line.Demo, err = writeDemo(plan, match.ID, result.Demo)
	}
	line.Seconds = time.Since(started).Seconds()
	if err != nil {
		line.Error = err.Error()
		return line
	}
	line.Outcome, line.Generations, line.Seats = &result.Outcome, result.Generations, result.Seats
	if result.Outcome.WinnerTeamColor != nil {
		line.Winner = &match.Seats[*result.Outcome.WinnerTeamColor-1]
	}
	return line
}

// writeDemo writes a match's demo into the schedule's demo directory, and
// returns its path.
func writeDemo(plan schedule, matchID string, demo models.Demo) (path string, err error) {
	path = filepath.Join(plan.DemoDirectory, matchID+".json")
	if plan.CompressDemos {
		path += ".gz"
	}
	file, err := os.Create(path)
	if err != nil {
		return "", err
	}
	defer func() {
		if closeError := file.Close(); err == nil {
			err = closeError
		}
	}()
	if !plan.CompressDemos {
		return path, services.WriteDemo(file, demo)
	}
	// The fastest level: a demo's lines repeat, so it still packs well, and
	// a tournament writes hundreds.
	compressor, err := gzip.NewWriterLevel(file, gzip.BestSpeed)
	if err != nil {
		return "", err
	}
	if err := services.WriteDemo(compressor, demo); err != nil {
		return "", err
	}
	return path, compressor.Close()
}

// workerStartTimeout is how long a worker has to load its bot and write its
// ready line. Tests shorten it.
var workerStartTimeout = 60 * time.Second

// playerSource gives each match a player for one of its seats, and takes it
// back when the match ends. ctx is the command's: a player isn't made ready
// once the command is told to stop.
type playerSource interface {
	seat(ctx context.Context) (player services.HeadlessPlayer, release func(), err error)
}

// defaultBot is a default bot, which plays any number of games at once.
type defaultBot struct{ bot bots.Bot }

func (source defaultBot) seat(context.Context) (services.HeadlessPlayer, func(), error) {
	return source.bot, func() {}, nil
}

// workerPool keeps a worker player's processes. A match borrows one process
// for each seat the player has in it, for the whole match, and a returned
// process plays the next match: a bot keeps nothing between turns, so any
// process plays any game. No more processes run than seats play at once.
type workerPool struct {
	name          string
	specification playerSpecification
	workerErrors  io.Writer
	lock          sync.Mutex
	idle          []*workerProcess
}

// seat borrows an idle process, or starts one.
func (pool *workerPool) seat(ctx context.Context) (services.HeadlessPlayer, func(), error) {
	pool.lock.Lock()
	var process *workerProcess
	if last := len(pool.idle) - 1; last >= 0 {
		process, pool.idle = pool.idle[last], pool.idle[:last]
	}
	pool.lock.Unlock()
	if process == nil {
		var err error
		if process, err = pool.start(ctx); err != nil {
			return nil, nil, err
		}
	}
	seat := &workerSeat{pool: pool, process: process}
	return seat, seat.release, nil
}

// workerSeat plays one seat of one match on a process of its pool. Its lock
// keeps a turn apart from the recovery after a failed one, from the next turn
// and from the release: a match that stops early may still have a turn
// running when it gives the seat back.
type workerSeat struct {
	pool *workerPool
	lock sync.Mutex
	// process is nil after a process was stopped, until Recover has started
	// its replacement, and for the rest of the match when that didn't start.
	process *workerProcess
	// startError is why the replacement didn't start.
	startError error
}

// Play has the seat's process play the turn. A process that stops, or whose
// turn runs out of time, can't play on, since its next line would answer the
// wrong turn: Play stops it, and Recover starts its replacement.
func (seat *workerSeat) Play(ctx context.Context, turn models.BotTurn) (models.BotTurnResult, error) {
	seat.lock.Lock()
	defer seat.lock.Unlock()
	if seat.process == nil {
		// One replacement is tried for each failed turn, never one for each
		// turn after it: a worker that can't start would cost every turn the
		// time it has to get ready.
		return models.BotTurnResult{}, fmt.Errorf("the worker of %s is not running: %w", seat.pool.name, seat.startError)
	}
	request, err := json.Marshal(map[string]models.BotTurn{"turn": turn})
	if err != nil {
		return models.BotTurnResult{}, err
	}
	answer, err := seat.process.exchange(ctx, request)
	if err != nil {
		seat.process.stop()
		seat.process, seat.startError = nil, errors.New("it was stopped after a failed turn")
		return models.BotTurnResult{}, err
	}
	if answer.Error != "" {
		return models.BotTurnResult{}, errors.New(answer.Error)
	}
	if answer.Result == nil {
		return models.BotTurnResult{}, errors.New("the worker answered neither a result nor an error")
	}
	return *answer.Result, nil
}

// Recover starts the replacement of a process that Play stopped, after the
// failed turn and outside its time, under the match's context. A worker that
// answered an error still runs, and needs none.
func (seat *workerSeat) Recover(ctx context.Context) {
	seat.lock.Lock()
	defer seat.lock.Unlock()
	if seat.process == nil {
		seat.process, seat.startError = seat.pool.start(ctx)
	}
}

// release gives the seat's process back to its pool, once no turn runs.
func (seat *workerSeat) release() {
	seat.lock.Lock()
	process := seat.process
	seat.process = nil
	seat.lock.Unlock()
	if process == nil {
		return
	}
	seat.pool.lock.Lock()
	defer seat.pool.lock.Unlock()
	seat.pool.idle = append(seat.pool.idle, process)
}

// start starts a process of the pool's command and waits for its ready line,
// for at most workerStartTimeout and not once ctx is done.
func (pool *workerPool) start(ctx context.Context) (*workerProcess, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	command := exec.Command(pool.specification.Worker[0], pool.specification.Worker[1:]...)
	command.Dir = pool.specification.Directory
	command.Env = os.Environ()
	for _, key := range slices.Sorted(maps.Keys(pool.specification.Environment)) {
		command.Env = append(command.Env, key+"="+pool.specification.Environment[key])
	}
	command.Stderr = pool.workerErrors
	ownProcessGroup(command)
	requests, err := command.StdinPipe()
	if err != nil {
		return nil, err
	}
	answers, err := command.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := command.Start(); err != nil {
		return nil, fmt.Errorf("start the worker of %s: %w", pool.name, err)
	}
	process := &workerProcess{command: command, requests: requests, answers: bufio.NewReader(answers)}
	ready, cancel := context.WithTimeout(ctx, workerStartTimeout)
	defer cancel()
	answer, err := process.read(ready)
	if err == nil && !answer.Ready {
		err = errors.New(answer.Error)
		if answer.Error == "" {
			err = errors.New("its first line was not {\"ready\": true}")
		}
	}
	if err != nil {
		process.stop()
		return nil, fmt.Errorf("the worker of %s didn't start: %w", pool.name, err)
	}
	return process, nil
}

// close stops the pool's processes. It is called once no match plays.
func (pool *workerPool) close() {
	pool.lock.Lock()
	defer pool.lock.Unlock()
	for _, process := range pool.idle {
		process.stop()
	}
	pool.idle = nil
}

// workerProcess is one running worker.
type workerProcess struct {
	command  *exec.Cmd
	requests io.WriteCloser
	answers  *bufio.Reader
	lock     sync.Mutex
	// ended is set once the process was waited for: its ID may then belong
	// to another process, which must not be killed.
	ended bool
}

// workerAnswer is one line a worker writes.
type workerAnswer struct {
	Ready  bool                  `json:"ready"`
	Result *models.BotTurnResult `json:"result"`
	Error  string                `json:"error"`
}

// exchange writes a request line and reads its answer. When ctx is done
// first it kills the process, so that neither a worker that doesn't read its
// requests nor one that doesn't answer can hold the turn: the write and the
// read both end. A caller that gets an error must stop the process.
func (process *workerProcess) exchange(ctx context.Context, request []byte) (workerAnswer, error) {
	cancelKill := context.AfterFunc(ctx, process.kill)
	defer cancelKill()
	if _, err := process.requests.Write(append(request, '\n')); err != nil {
		if ctx.Err() != nil {
			return workerAnswer{}, ctx.Err()
		}
		return workerAnswer{}, fmt.Errorf("the worker stopped: %w", err)
	}
	return process.read(ctx)
}

// read reads one line of the worker, or fails when ctx is done first. A
// caller that gets an error must stop the process: the read may still be
// waiting for the line.
func (process *workerProcess) read(ctx context.Context) (workerAnswer, error) {
	type readLine struct {
		line []byte
		err  error
	}
	lines := make(chan readLine, 1)
	go func() {
		line, err := process.answers.ReadBytes('\n')
		lines <- readLine{line: line, err: err}
	}()
	select {
	case <-ctx.Done():
		return workerAnswer{}, ctx.Err()
	case read := <-lines:
		if read.err != nil {
			return workerAnswer{}, fmt.Errorf("the worker stopped: %w", read.err)
		}
		var answer workerAnswer
		if err := json.Unmarshal(read.line, &answer); err != nil {
			return workerAnswer{}, fmt.Errorf("the worker's line is not JSON: %w", err)
		}
		return answer, nil
	}
}

// kill kills the process, and the processes it started: a worker command may
// be a wrapper, such as a shell or `uv run`, around the bot's own process.
func (process *workerProcess) kill() {
	process.lock.Lock()
	defer process.lock.Unlock()
	if !process.ended {
		killWithChildren(process.command)
	}
}

// stop ends the process and waits for it.
func (process *workerProcess) stop() {
	process.kill()
	_ = process.requests.Close()
	_ = process.command.Wait()
	process.lock.Lock()
	process.ended = true
	process.lock.Unlock()
}
