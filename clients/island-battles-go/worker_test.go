package islandbattles

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// workerBot prints, logs, counts its turns, and fails on demand.
func workerBot(ctx context.Context, turn Turn) (Result, error) {
	fmt.Println("printing to stdout must not reach the protocol")
	Logger(ctx).Info("logged line")
	var remembered struct {
		Turns int `json:"turns"`
	}
	_ = turn.Memory.Decode(&remembered)
	remembered.Turns++
	switch remembered.Turns {
	case 100:
		return Result{}, errors.New("the bot is broken")
	case 101:
		panic("the bot panicked")
	case 102:
		return Result{Memory: Memory(`{"blob":"` + strings.Repeat("x", 70000) + `"}`)}, nil
	}
	memory, err := NewMemory(remembered)
	return Result{Logs: []LogLine{Log("returned line")}, Memory: memory}, err
}

// turnLine is a request line for the fixture turn with turns already played.
func turnLine(t *testing.T, turns int) string {
	t.Helper()
	var turn map[string]any
	if err := json.Unmarshal(fixtureTurnJSON, &turn); err != nil {
		t.Fatal(err)
	}
	turn["memory"] = map[string]int{"turns": turns}
	line, err := json.Marshal(map[string]any{"turn": turn})
	if err != nil {
		t.Fatal(err)
	}
	return string(line)
}

// answers splits the worker's output into lines, each decoded.
func answers(t *testing.T, output string) []map[string]json.RawMessage {
	t.Helper()
	var all []map[string]json.RawMessage
	for _, line := range strings.Split(strings.TrimSuffix(output, "\n"), "\n") {
		var answer map[string]json.RawMessage
		if err := json.Unmarshal([]byte(line), &answer); err != nil {
			t.Fatalf("protocol line %q is not JSON: %v", line, err)
		}
		all = append(all, answer)
	}
	return all
}

func runWorker(t *testing.T, input string) (int, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	status := runMain(context.Background(), invocation{name: "mybot", play: workerBot, stdin: strings.NewReader(input), stdout: &stdout, stderr: &stderr}, []string{"worker"})
	return status, stdout.String(), stderr.String()
}

func TestWorkerAnswersTurnsAndExitsOnEOF(t *testing.T) {
	status, stdout, stderr := runWorker(t, turnLine(t, 0)+"\n\n"+turnLine(t, 5)+"\n")
	if status != 0 {
		t.Fatalf("got status %d, stderr %q", status, stderr)
	}
	got := answers(t, stdout)
	if len(got) != 3 || string(got[0]["ready"]) != "true" {
		t.Fatalf("got %q", stdout)
	}
	var first, second struct {
		Memory map[string]int `json:"memory"`
		Logs   []LogLine      `json:"logs"`
	}
	if err := json.Unmarshal(got[1]["result"], &first); err != nil || first.Memory["turns"] != 1 {
		t.Fatalf("got %s, %v", got[1]["result"], err)
	}
	if err := json.Unmarshal(got[2]["result"], &second); err != nil || second.Memory["turns"] != 6 {
		t.Fatalf("got %s, %v", got[2]["result"], err)
	}
	if len(first.Logs) != 2 || first.Logs[0].Message != "logged line" || first.Logs[1].Message != "returned line" {
		t.Fatalf("the logger line comes first: got %+v", first.Logs)
	}
	if !strings.Contains(stderr, "logged line") {
		t.Fatalf("the bot's log goes to stderr: got %q", stderr)
	}
}

func TestWorkerReportsFailuresAsErrorLinesAndGoesOn(t *testing.T) {
	input := strings.Join([]string{turnLine(t, 99), turnLine(t, 100), turnLine(t, 0), "not json", `{"nothing": 1}`, `{"turn": {}}`}, "\n") + "\n"
	status, stdout, stderr := runWorker(t, input)
	if status != 0 {
		t.Fatalf("got status %d, stderr %q", status, stderr)
	}
	got := answers(t, stdout)
	if len(got) != 7 {
		t.Fatalf("got %q", stdout)
	}
	wanted := []string{"the bot is broken", "the bot panicked", "", "not a JSON object", "must be an object", "not a BotTurn"}
	for index, text := range wanted {
		answer := got[index+1]
		if text == "" {
			if _, found := answer["result"]; !found {
				t.Fatalf("line %d: got %v", index+1, answer)
			}
			continue
		}
		var message string
		if err := json.Unmarshal(answer["error"], &message); err != nil || !strings.Contains(message, text) {
			t.Fatalf("line %d: got %v, want an error with %q", index+1, answer, text)
		}
		if _, found := answer["result"]; found {
			t.Fatalf("line %d has both: %v", index+1, answer)
		}
	}
}

func TestWorkerRefusesMemoryOverTheLimit(t *testing.T) {
	_, stdout, _ := runWorker(t, turnLine(t, 101)+"\n")
	var message string
	if err := json.Unmarshal(answers(t, stdout)[1]["error"], &message); err != nil || !strings.Contains(strings.ToLower(message), "memory") {
		t.Fatalf("got %q", stdout)
	}
}

// TestWorkerHelper is the bot program of TestWorkerKeepsTheProtocolClean.
func TestWorkerHelper(t *testing.T) {
	if os.Getenv("ISLAND_BATTLES_WORKER_HELPER") != "1" {
		t.Skip("only the helper process runs this")
	}
	os.Args = []string{"mybot", "worker"}
	Main(workerBot)
}

// A real process, so that the real stdout is the protocol's: the bot's print
// goes to stderr.
func TestWorkerKeepsTheProtocolClean(t *testing.T) {
	command := exec.Command(os.Args[0], "-test.run=^TestWorkerHelper$")
	command.Env = append(os.Environ(), "ISLAND_BATTLES_WORKER_HELPER=1")
	command.Stdin = strings.NewReader(turnLine(t, 0) + "\n")
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	if err := command.Run(); err != nil {
		t.Fatalf("%v, stderr %q", err, stderr.String())
	}
	got := answers(t, stdout.String())
	if len(got) != 2 || string(got[0]["ready"]) != "true" || got[1]["result"] == nil {
		t.Fatalf("stdout is not clean: %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "printing to stdout") {
		t.Fatalf("the print goes to stderr: got %q", stderr.String())
	}
}
