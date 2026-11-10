package islandbattles

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

// run runs the command line of a bot program and returns its status, stdout
// and stderr.
func run(t *testing.T, play Play, arguments ...string) (int, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	status := runMain(context.Background(), invocation{name: "mybot", play: play, stdout: &stdout, stderr: &stderr}, arguments)
	return status, stdout.String(), stderr.String()
}

// sender sends one soldier and remembers a counter.
func sender(_ context.Context, turn Turn) (Result, error) {
	memory, err := NewMemory(map[string]int{"turns": 1})
	return Result{Commands: []Command{SendSoldierGroup(0, 1, 1)}, Logs: []LogLine{Log("sending")}, Memory: memory}, err
}

func TestCheckReportsAGoodBot(t *testing.T) {
	status, stdout, stderr := run(t, sender, "check")
	if status != 0 || !regexp.MustCompile(`^ok: 1 commands, 1 log lines, memory 11 of 65536 bytes, \d+\.\d\d s\n$`).MatchString(stdout) {
		t.Fatalf("got status %d, stdout %q, stderr %q", status, stdout, stderr)
	}
	empty := func(context.Context, Turn) (Result, error) { return Result{}, nil }
	if status, stdout, _ := run(t, empty, "check"); status != 0 || !strings.HasPrefix(stdout, "ok: 0 commands, 0 log lines, memory 4 of 65536 bytes") {
		t.Fatalf("got status %d, stdout %q", status, stdout)
	}
}

func TestCheckPlaysATurnFile(t *testing.T) {
	file := filepath.Join(t.TempDir(), "turn.json")
	turn := strings.Replace(string(fixtureTurnJSON), `"team_color": 1,`, `"team_color": 2,`, 1)
	if err := os.WriteFile(file, []byte(turn), 0o600); err != nil {
		t.Fatal(err)
	}
	var team uint
	play := func(_ context.Context, turn Turn) (Result, error) {
		team = turn.TeamColor
		return Result{}, nil
	}
	if status, _, stderr := run(t, play, "check", "--turn", file); status != 0 || team != 2 {
		t.Fatalf("got status %d, team %d, stderr %q", status, team, stderr)
	}
	if err := os.WriteFile(file, []byte(`{"game_id": 1}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if status, _, stderr := run(t, play, "check", "--turn", file); status != 1 || !strings.Contains(stderr, "is not a BotTurn: turn.generation is missing") {
		t.Fatalf("got status %d, stderr %q", status, stderr)
	}
	if status, _, stderr := run(t, play, "check", "--turn", file+".missing"); status != 1 || !strings.HasPrefix(stderr, "FAIL: could not read the turn") {
		t.Fatalf("got status %d, stderr %q", status, stderr)
	}
}

func TestCheckFailsABadBot(t *testing.T) {
	checkTimeout = 200 * time.Millisecond
	t.Cleanup(func() { checkTimeout = 2 * time.Second })
	for want, play := range map[string]Play{
		"FAIL: the bot returned an error: no plan\n": func(context.Context, Turn) (Result, error) { return Result{}, errors.New("no plan") },
		"FAIL: the bot panicked: panic: boom":        func(context.Context, Turn) (Result, error) { panic("boom") },
		"FAIL: the bot returned an invalid result: invalid result: commands[0]: soldier_amount must be at least 1\n": func(context.Context, Turn) (Result, error) {
			return Result{Commands: []Command{SendSoldierGroup(0, 1, 0)}}, nil
		},
		"FAIL: memory is too large: memory is 65544 bytes as compact JSON; the limit is 65536 (64 KiB)\n": func(context.Context, Turn) (Result, error) {
			return Result{Memory: Memory(`{"a":"` + strings.Repeat("x", MemoryLimitBytes) + `"}`)}, nil
		},
		"FAIL: the bot took longer than 0.2 s on one turn\n": func(context.Context, Turn) (Result, error) {
			time.Sleep(time.Second)
			return Result{}, nil
		},
	} {
		if status, stdout, stderr := run(t, play, "check"); status != 1 || stdout != "" || !strings.HasPrefix(stderr, want) {
			t.Errorf("got status %d, stdout %q, stderr %q; want %q", status, stdout, stderr, want)
		}
	}
}

func TestFixturePrintsTheFixtureTurn(t *testing.T) {
	status, stdout, _ := run(t, sender, "fixture")
	if status != 0 || stdout != string(fixtureTurnJSON) {
		t.Fatalf("got status %d and %q", status, stdout)
	}
	if _, err := DecodeTurn([]byte(stdout)); err != nil {
		t.Fatal(err)
	}
}

func TestMainUsage(t *testing.T) {
	if status, _, stderr := run(t, sender); status != 2 || !strings.Contains(stderr, "Usage: mybot <command> [flags]") || !strings.Contains(stderr, "serve ") {
		t.Fatalf("got status %d, stderr %q", status, stderr)
	}
	if status, _, stderr := run(t, sender, "play"); status != 2 || !strings.Contains(stderr, `unknown command "play"`) {
		t.Fatalf("got status %d, stderr %q", status, stderr)
	}
	if status, _, _ := run(t, sender, "-h"); status != 0 {
		t.Fatalf("got status %d", status)
	}
	if status, _, stderr := run(t, sender, "check", "-h"); status != 0 || !strings.Contains(stderr, "-turn file") {
		t.Fatalf("got status %d, stderr %q", status, stderr)
	}
	if status, _, _ := run(t, sender, "check", "--nope"); status != 2 {
		t.Fatalf("got status %d", status)
	}
	if status, _, _ := run(t, sender, "fixture", "extra"); status != 2 {
		t.Fatalf("got status %d", status)
	}
	if status, _, _ := run(t, sender, "serve", "--turn-timeout", "0s"); status != 2 {
		t.Fatalf("got status %d", status)
	}
}
