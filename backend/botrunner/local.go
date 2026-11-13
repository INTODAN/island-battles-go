package botrunner

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"islandBattles/backend/botrunner/native"
	pythonrunner "islandBattles/backend/botrunner/python"
	"islandBattles/backend/models"
)

// localTurnTimeout bounds one local turn, as the functions' 20-second
// timeout bounds an invocation.
const localTurnTimeout = 20 * time.Second

// maxLocalPackages is how many packages Local keeps loaded at once; the
// least recently used idle one is unloaded to make room.
const maxLocalPackages = 8

// packageIDPattern is what a package ID may look like in a folder name.
var packageIDPattern = regexp.MustCompile(`^[A-Za-z0-9-]{1,64}$`)

// Local runs the bot runners in this process, for the local server host
// when no runner functions are configured. Like a Lambda instance, it keeps
// each package loaded between turns and plays one turn of a package at a
// time; different packages play at once. A hosted bot's delivery may go to
// this machine, such as to island-battles-bot serve, which the deployed
// runner refuses.
type Local struct {
	// SubmitHost is the local game's host, such as localhost:8888: the only
	// one the runners submit to, as GAME_SUBMIT_HOST on Lambda.
	SubmitHost string
	// Python is the command that runs Python 3.14, such as
	// ["python3.14"], for runner.py and the packages' workers.
	Python []string
	// Directory holds the loaded packages and runner.py; Close deletes it.
	Directory string
	// Logger gets a line for each turn played.
	Logger *slog.Logger

	mutex     sync.Mutex
	instances map[string]*localInstance
	closed    bool
	turns     sync.WaitGroup
}

// localInstance is one loaded package, like one Lambda instance.
type localInstance struct {
	busy     sync.Mutex
	lastUsed time.Time
	native   *native.Runner
	python   *pythonProcess
}

// Invoke plays the event's turn in a goroutine, and returns at once.
func (local *Local) Invoke(_ context.Context, runner models.BotRunnerName, event models.BotRunnerEvent) error {
	if runner != models.BotRunnerPython && runner != models.BotRunnerNative {
		return fmt.Errorf("there is no %q bot runner", runner)
	}
	if event.Hosted != nil && runner != models.BotRunnerNative {
		return errors.New("only the native runner delivers hosted bots' turns")
	}
	if event.Hosted == nil && (event.Package == nil || !packageIDPattern.MatchString(event.Package.ID)) {
		return errors.New("the event has no package with a plain ID")
	}
	payload, err := json.Marshal(event)
	if err != nil {
		return err
	}
	local.mutex.Lock()
	defer local.mutex.Unlock()
	if local.closed {
		return errors.New("the local bot runners have stopped")
	}
	local.turns.Add(1)
	go func() {
		defer local.turns.Done()
		local.play(runner, event, payload)
	}()
	return nil
}

func (local *Local) play(runner models.BotRunnerName, event models.BotRunnerEvent, payload []byte) {
	ctx, cancel := context.WithTimeout(context.Background(), localTurnTimeout)
	defer cancel()
	attributes := []any{"runner", runner, "game_id", event.Delivery.Turn.GameID, "generation_id", event.Delivery.Turn.Generation.ID}
	var outcome any
	var err error
	if event.Hosted != nil {
		hosted := &native.Runner{SubmitHost: local.SubmitHost, AllowLoopbackHosted: true, Logger: local.logger()}
		outcome, err = hosted.Handle(ctx, payload)
	} else {
		instance, found := local.instance(runner, event.Package.ID)
		if !found {
			local.logger().Warn("The local bot runners have stopped", attributes...)
			return
		}
		defer instance.busy.Unlock()
		if instance.native != nil {
			outcome, err = instance.native.Handle(ctx, payload)
		} else {
			// The Python runner answers a JSON line, logged as text.
			var answer json.RawMessage
			answer, err = local.playPython(instance, payload)
			outcome = strings.TrimSpace(string(answer))
		}
	}
	if err != nil {
		local.logger().Warn("A local bot runner refused a turn", append(attributes, "error", err)...)
		return
	}
	local.logger().Info("A local bot runner played a turn", append(attributes, "outcome", outcome)...)
}

// instance returns the runner's instance with the package, locked for one
// turn, starting one if there is none, and unloading the least recently
// used idle one past maxLocalPackages. It waits while the package plays
// another turn.
func (local *Local) instance(runner models.BotRunnerName, packageID string) (*localInstance, bool) {
	key := string(runner) + "-" + packageID
	local.mutex.Lock()
	if local.closed {
		local.mutex.Unlock()
		return nil, false
	}
	if local.instances == nil {
		local.instances = map[string]*localInstance{}
	}
	instance, found := local.instances[key]
	if !found {
		instance = &localInstance{}
		if runner == models.BotRunnerNative {
			instance.native = &native.Runner{Directory: filepath.Join(local.Directory, key), SubmitHost: local.SubmitHost,
				AllowHTTPPackages: true, AllowLoopbackHosted: true, Logger: local.logger()}
		}
		local.instances[key] = instance
		local.evict(key)
	}
	instance.lastUsed = time.Now()
	local.mutex.Unlock()
	instance.busy.Lock()
	return instance, true
}

// evict unloads the least recently used idle instance but keep, while
// there are more than maxLocalPackages. It runs under local.mutex.
func (local *Local) evict(keep string) {
	for len(local.instances) > maxLocalPackages {
		oldestKey := ""
		for key, instance := range local.instances {
			if key != keep && (oldestKey == "" || instance.lastUsed.Before(local.instances[oldestKey].lastUsed)) && instance.busy.TryLock() {
				if oldestKey != "" {
					local.instances[oldestKey].busy.Unlock()
				}
				oldestKey = key
			}
		}
		if oldestKey == "" {
			return
		}
		local.instances[oldestKey].unload()
		local.instances[oldestKey].busy.Unlock()
		delete(local.instances, oldestKey)
	}
}

func (instance *localInstance) unload() {
	if instance.native != nil {
		instance.native.Unload()
	}
	if instance.python != nil {
		instance.python.stop()
		instance.python = nil
	}
}

// Close stops every runner, waits for the turns being played, and deletes
// the loaded packages.
func (local *Local) Close() {
	local.mutex.Lock()
	local.closed = true
	local.mutex.Unlock()
	local.turns.Wait()
	local.mutex.Lock()
	defer local.mutex.Unlock()
	for key, instance := range local.instances {
		instance.unload()
		delete(local.instances, key)
	}
	_ = os.RemoveAll(local.Directory)
}

func (local *Local) logger() *slog.Logger {
	if local.Logger != nil {
		return local.Logger
	}
	return slog.Default()
}

// pythonProcess is runner.py --events: it plays one event per line, keeping
// its package loaded between them.
type pythonProcess struct {
	command *exec.Cmd
	stdin   io.WriteCloser
	stdout  *bufio.Reader
}

// playPython plays a turn with the instance's runner.py, starting it first
// if it isn't running, and returns its answer line.
func (local *Local) playPython(instance *localInstance, payload []byte) (json.RawMessage, error) {
	if instance.python == nil {
		process, err := local.startPython()
		if err != nil {
			return nil, err
		}
		instance.python = process
	}
	answer, err := instance.python.play(payload)
	if err != nil {
		// The next turn starts it again.
		instance.python.stop()
		instance.python = nil
		return nil, err
	}
	var refused struct {
		Error string `json:"error"`
	}
	if json.Unmarshal(answer, &refused) == nil && refused.Error != "" {
		return nil, errors.New(refused.Error)
	}
	return answer, nil
}

func (local *Local) startPython() (*pythonProcess, error) {
	if len(local.Python) == 0 {
		return nil, errors.New("no Python command is configured for the local Python bot runner")
	}
	script := filepath.Join(local.Directory, "runner.py")
	if err := os.MkdirAll(local.Directory, 0o700); err != nil {
		return nil, err
	}
	if err := os.WriteFile(script, pythonrunner.Runner, 0o600); err != nil {
		return nil, err
	}
	arguments := append(append([]string{}, local.Python[1:]...), "-I", script, "--events")
	command := exec.Command(local.Python[0], arguments...)
	// The runner gets only what it needs, as on Lambda: none of the
	// backend's secrets.
	command.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + os.Getenv("HOME"), "LANG=C.UTF-8", "TMPDIR=" + os.TempDir(),
		"GAME_SUBMIT_HOST=" + local.SubmitHost}
	command.Stderr = os.Stderr
	stdin, err := command.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := command.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := command.Start(); err != nil {
		return nil, fmt.Errorf("start the local Python bot runner with %q: %w", local.Python[0], err)
	}
	return &pythonProcess{command: command, stdin: stdin, stdout: bufio.NewReaderSize(stdout, 64<<10)}, nil
}

func (process *pythonProcess) play(payload []byte) (json.RawMessage, error) {
	if _, err := process.stdin.Write(append(payload, '\n')); err != nil {
		return nil, fmt.Errorf("the local Python bot runner stopped: %w", err)
	}
	line, err := process.stdout.ReadBytes('\n')
	if err != nil {
		return nil, fmt.Errorf("the local Python bot runner stopped: %w", err)
	}
	return json.RawMessage(line), nil
}

func (process *pythonProcess) stop() {
	_ = process.stdin.Close()
	done := make(chan struct{})
	go func() {
		_ = process.command.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		_ = process.command.Process.Kill()
		<-done
	}
}
