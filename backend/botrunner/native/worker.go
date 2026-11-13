package native

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	// maxAnswerBytes bounds one line of the worker's output. A result is at
	// most 1000 commands, 100 log lines and 64 KiB of memory, well under it.
	maxAnswerBytes = 4 << 20
	// stderrTailBytes is how much of the worker's stderr is kept for the
	// game log line of a bot that fails.
	stderrTailBytes = 4 << 10
	// exitWait is how long a worker that closed its stdout gets to exit, so
	// its exit status and last stderr lines are known.
	exitWait = time.Second
)

var errLineTooLong = errors.New("wrote a line longer than 4 MiB")

// worker is a bot's worker process (#216): the runner writes {"turn": ...}
// lines to its stdin and reads one answer line from its stdout for each.
type worker struct {
	packageID string
	command   *exec.Cmd
	stdin     io.WriteCloser
	lines     chan workerLine
	stderr    *outputTail
	// stop ends the reader once the worker is killed, and exited is closed
	// once the process has been waited for.
	stop     chan struct{}
	stopOnce sync.Once
	exited   chan struct{}
	exitErr  error
}

type workerLine struct {
	data []byte
	err  error
}

// botAnswerError is a turn the bot itself failed: the worker answered
// {"error": ...} and keeps running.
type botAnswerError struct{ message string }

func (answerError *botAnswerError) Error() string { return answerError.message }

// startWorker starts the program in directory, in a process group of its
// own so that killing it kills what it started too, with only the given
// environment.
func startWorker(packageID, directory string, arguments, environment []string) (*worker, error) {
	command := exec.Command(arguments[0], arguments[1:]...)
	command.Dir = directory
	command.Env = environment
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	tail := &outputTail{}
	command.Stderr = tail
	// A child of the bot that keeps stderr open must not keep Wait waiting.
	command.WaitDelay = exitWait
	stdin, err := command.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := command.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := command.Start(); err != nil {
		return nil, err
	}
	started := &worker{packageID: packageID, command: command, stdin: stdin, lines: make(chan workerLine), stderr: tail,
		stop: make(chan struct{}), exited: make(chan struct{})}
	go started.read(stdout)
	return started, nil
}

// read passes the worker's output lines on until it ends or the worker is
// stopped, then waits for the process.
func (worker *worker) read(stdout io.Reader) {
	defer func() {
		close(worker.lines)
		worker.exitErr = worker.command.Wait()
		close(worker.exited)
	}()
	reader := bufio.NewReaderSize(stdout, 64<<10)
	for {
		line, err := readLine(reader)
		if len(bytes.TrimSpace(line)) == 0 && err == nil {
			continue
		}
		if len(bytes.TrimSpace(line)) > 0 || errors.Is(err, errLineTooLong) {
			select {
			case worker.lines <- workerLine{data: line, err: err}:
			case <-worker.stop:
				return
			}
		}
		if err != nil {
			return
		}
	}
}

// readLine reads one line of at most maxAnswerBytes.
func readLine(reader *bufio.Reader) ([]byte, error) {
	var line []byte
	for {
		chunk, err := reader.ReadSlice('\n')
		if len(line)+len(chunk) > maxAnswerBytes {
			return nil, errLineTooLong
		}
		line = append(line, chunk...)
		if errors.Is(err, bufio.ErrBufferFull) {
			continue
		}
		return line, err
	}
}

// stopped reports whether the worker has exited.
func (worker *worker) stopped() bool {
	select {
	case <-worker.exited:
		return true
	default:
		return false
	}
}

// kill kills the worker's whole process group and waits for it.
func (worker *worker) kill() {
	_ = worker.stdin.Close()
	if worker.command.Process != nil {
		_ = syscall.Kill(-worker.command.Process.Pid, syscall.SIGKILL)
	}
	worker.stopOnce.Do(func() { close(worker.stop) })
	select {
	case <-worker.exited:
	case <-time.After(2 * exitWait):
	}
}

// ended describes a worker whose output ended: how it exited.
func (worker *worker) ended() error {
	select {
	case <-worker.exited:
	case <-time.After(exitWait):
		return errors.New("closed its output")
	}
	if worker.exitErr == nil {
		return errors.New("exited")
	}
	return fmt.Errorf("stopped (%v)", worker.exitErr)
}

// waitReady waits for the worker's {"ready": true} line until ctx ends.
func (worker *worker) waitReady(ctx context.Context) error {
	select {
	case line, open := <-worker.lines:
		if !open {
			return fmt.Errorf("its worker %v before it was ready", worker.ended())
		}
		if line.err != nil {
			return fmt.Errorf("its worker %v", line.err)
		}
		var answer struct {
			Ready bool    `json:"ready"`
			Error *string `json:"error"`
		}
		if err := json.Unmarshal(line.data, &answer); err != nil {
			return fmt.Errorf("its worker wrote %q instead of {\"ready\": true}", firstCharacters(line.data))
		}
		if answer.Error != nil {
			return errors.New(*answer.Error)
		}
		if !answer.Ready {
			return fmt.Errorf("its worker wrote %q instead of {\"ready\": true}", firstCharacters(line.data))
		}
		return nil
	case <-ctx.Done():
		return errors.New("it was not ready within the time to load it")
	}
}

// play writes the turn and waits at most limit for the answer. It returns
// the result, a *botAnswerError when the bot answered with an error, or an
// error that says how the worker failed; the caller kills it then.
func (worker *worker) play(ctx context.Context, turn json.RawMessage, limit time.Duration) (json.RawMessage, error) {
	request := make([]byte, 0, len(turn)+11)
	request = append(append(append(request, `{"turn":`...), turn...), "}\n"...)
	// The write blocks while the worker doesn't read, so it runs on its own;
	// killing the worker ends it.
	go func() { _, _ = worker.stdin.Write(request) }()
	timer := time.NewTimer(limit)
	defer timer.Stop()
	select {
	case line, open := <-worker.lines:
		if !open {
			return nil, worker.ended()
		}
		if line.err != nil {
			return nil, fmt.Errorf("broke the worker protocol: it %v", line.err)
		}
		var answer struct {
			Result json.RawMessage `json:"result"`
			Error  *string         `json:"error"`
		}
		if err := json.Unmarshal(line.data, &answer); err != nil || (answer.Error == nil && len(answer.Result) == 0) {
			return nil, fmt.Errorf("broke the worker protocol: it wrote %q", firstCharacters(line.data))
		}
		if answer.Error != nil {
			return nil, &botAnswerError{message: *answer.Error}
		}
		return answer.Result, nil
	case <-timer.C:
		return nil, fmt.Errorf("did not answer within %v", limit)
	case <-ctx.Done():
		return nil, errors.New("did not answer before the invocation's time ran out")
	}
}

func firstCharacters(data []byte) string {
	text := []rune(strings.TrimSpace(string(data)))
	if len(text) > 80 {
		return string(text[:79]) + "…"
	}
	return string(text)
}

// outputTail keeps the last stderrTailBytes of what is written to it.
type outputTail struct {
	mutex sync.Mutex
	data  []byte
}

func (tail *outputTail) Write(data []byte) (int, error) {
	tail.mutex.Lock()
	defer tail.mutex.Unlock()
	tail.data = append(tail.data, data...)
	if len(tail.data) > stderrTailBytes {
		tail.data = append([]byte(nil), tail.data[len(tail.data)-stderrTailBytes:]...)
	}
	return len(data), nil
}

// String is the kept output, starting at a whole line when it was cut.
func (tail *outputTail) String() string {
	tail.mutex.Lock()
	defer tail.mutex.Unlock()
	text := string(bytes.ToValidUTF8(tail.data, nil))
	if len(tail.data) >= stderrTailBytes {
		if index := strings.IndexByte(text, '\n'); index >= 0 {
			text = text[index+1:]
		}
	}
	return text
}

// workerEnvironment is the only environment a worker gets: no AWS
// credentials or other settings of the runner's. The runtime's library path
// is kept, for a program that links shared libraries.
func workerEnvironment() []string {
	temporary := os.TempDir()
	environment := []string{"PATH=/usr/local/bin:/usr/bin:/bin", "HOME=" + temporary, "TMPDIR=" + temporary, "LANG=C.UTF-8"}
	if libraries, found := os.LookupEnv("LD_LIBRARY_PATH"); found {
		environment = append(environment, "LD_LIBRARY_PATH="+libraries)
	}
	return environment
}
