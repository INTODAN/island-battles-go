package main

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"islandBattles/backend/models"
)

// workerEnvironment makes this test binary a worker instead of running the
// tests, so the tests have a worker command on every system: it answers each
// turn with one send from the team's first castle to the first castle that
// isn't the team's. As "broken" it answers an error instead; as "slow" it
// takes a second for turn 3, and as "dying" it exits on turn 3.
const workerEnvironment = "HEADLESS_MATCH_TEST_WORKER"

func TestMain(tests *testing.M) {
	if behaviour := os.Getenv(workerEnvironment); behaviour != "" {
		os.Exit(testWorker(behaviour))
	}
	os.Exit(tests.Run())
}

func testWorker(behaviour string) int {
	fmt.Println(`{"ready": true}`)
	lines := bufio.NewReader(os.Stdin)
	for {
		line, err := lines.ReadBytes('\n')
		if err != nil {
			return 0
		}
		var request struct {
			Turn models.BotTurn `json:"turn"`
		}
		if err := json.Unmarshal(line, &request); err != nil {
			return 1
		}
		switch {
		case behaviour == "broken":
			fmt.Println(`{"error": "no plan"}`)
			continue
		case behaviour == "slow" && request.Turn.TurnNumber == 3:
			time.Sleep(time.Second)
		case behaviour == "dying" && request.Turn.TurnNumber == 3:
			return 3
		}
		commands := []map[string]any{}
		var origin, destination *models.CastleView
		for index, castle := range request.Turn.Generation.Castles {
			switch {
			case castle.TeamColor == request.Turn.TeamColor && origin == nil && castle.Soldiers > 1:
				origin = &request.Turn.Generation.Castles[index]
			case castle.TeamColor != request.Turn.TeamColor && destination == nil:
				destination = &request.Turn.Generation.Castles[index]
			}
		}
		if origin != nil && destination != nil {
			commands = append(commands, map[string]any{"command_type": "send_soldier_group",
				"parameters": map[string]uint{"origin_castle_id": origin.ID, "destination_castle_id": destination.ID, "soldier_amount": origin.Soldiers - 1}})
		}
		answer, err := json.Marshal(map[string]any{"result": map[string]any{"commands": commands, "memory": map[string]int{"turn": request.Turn.TurnNumber},
			"logs": []map[string]string{{"level": "info", "logger": "island_battles.player", "message": "sent what I had"}}}})
		if err != nil {
			return 1
		}
		fmt.Println(string(answer))
	}
}

const testMapFile = `{"map_name": "duel", "player_amount": 2, "plane_size_x": 8, "plane_size_y": 6, "neutral_starting_soldier_amount": 5,
	"castles": [{"team_color": 1, "x": -2, "y": 0, "starting_soldier_amount": 30}, {"team_color": 2, "x": 2, "y": 0, "starting_soldier_amount": 30}, {"team_color": 0, "x": 0, "y": 2}]}`

// playSchedule runs the command on a schedule with the test map and the
// given players and matches, and returns its lines by match ID.
func playSchedule(t *testing.T, players map[string]any, matches []map[string]any, settings map[string]any) (map[string]matchLine, string) {
	t.Helper()
	directory := t.TempDir()
	mapPath := filepath.Join(directory, "duel.json")
	if err := os.WriteFile(mapPath, []byte(testMapFile), 0o644); err != nil {
		t.Fatal(err)
	}
	plan := map[string]any{"players": players, "maps": map[string]string{"duel": mapPath}, "matches": matches, "demo_directory": filepath.Join(directory, "demos")}
	for key, value := range settings {
		plan[key] = value
	}
	input, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := run(context.Background(), nil, bytes.NewReader(input), &output, io.Discard); err != nil {
		t.Fatal(err)
	}
	lines := map[string]matchLine{}
	for _, raw := range strings.Split(strings.TrimSpace(output.String()), "\n") {
		var line matchLine
		if err := json.Unmarshal([]byte(raw), &line); err != nil {
			t.Fatalf("line %q: %v", raw, err)
		}
		lines[line.ID] = line
	}
	return lines, directory
}

func testWorkerPlayer(behaviour string) map[string]any {
	return map[string]any{"worker": []string{os.Args[0]}, "environment": map[string]string{workerEnvironment: behaviour}}
}

func TestRunPlaysEveryMatchAndWritesItsDemo(t *testing.T) {
	level := uint(1)
	players := map[string]any{"level1": map[string]any{"default_level": level}, "sender": testWorkerPlayer("sends"), "broken": testWorkerPlayer("broken")}
	matches := []map[string]any{
		{"id": "sender-first", "map": "duel", "seats": []string{"sender", "level1"}},
		{"id": "sender-second", "map": "duel", "seats": []string{"level1", "sender"}},
		{"id": "sender-broken", "map": "duel", "seats": []string{"sender", "broken"}},
		{"id": "sender-sender", "map": "duel", "seats": []string{"sender", "sender"}},
	}
	lines, directory := playSchedule(t, players, matches, map[string]any{"max_generations": 60, "parallel": 3, "compress_demos": true})
	if len(lines) != len(matches) {
		t.Fatalf("%d lines, want %d", len(lines), len(matches))
	}
	for id, line := range lines {
		if line.Error != "" || line.Outcome == nil || line.Generations < 2 || len(line.Seats) != 2 {
			t.Fatalf("%s: %+v, want a played match", id, line)
		}
		file, err := os.Open(filepath.Join(directory, "demos", id+".json.gz"))
		if err != nil {
			t.Fatal(err)
		}
		unpacked, err := gzip.NewReader(file)
		if err != nil {
			t.Fatal(err)
		}
		var demo models.Demo
		if err := json.NewDecoder(unpacked).Decode(&demo); err != nil {
			t.Fatalf("%s: the demo doesn't read: %v", id, err)
		}
		_ = file.Close()
		if len(demo.Frames) != line.Generations || demo.Outcome == nil || demo.Teams[0].Name != line.Players[0] {
			t.Fatalf("%s: the demo has %d frames and teams %+v, want %d frames of %v", id, len(demo.Frames), demo.Teams, line.Generations, line.Players)
		}
	}
	broken := lines["sender-broken"]
	if broken.Seats[1].FailedTurns != broken.Seats[1].TurnsPlayed || broken.Seats[1].Failures[0].Message != "no plan" || broken.Seats[0].FailedTurns != 0 {
		t.Fatalf("sender against broken: %+v, want every turn of broken to fail with its message, and none of sender's", broken)
	}
	if lines["sender-first"].Seats[0].Commands == 0 || lines["sender-second"].Seats[1].Commands == 0 {
		t.Fatalf("the worker's commands were not played: %+v %+v", lines["sender-first"].Seats[0], lines["sender-second"].Seats[1])
	}
}

// A worker whose turn fails is stopped, and its replacement is started before
// the seat's next turn: only the failed turn is lost, in that match and in
// the matches that get the process afterwards.
func TestRunLosesOnlyTheTurnOfAWorkerThatIsLateOrStops(t *testing.T) {
	level := uint(1)
	for behaviour, message := range map[string]string{"slow": "the bot didn't answer within 200ms", "dying": "the worker stopped"} {
		t.Run(behaviour, func(t *testing.T) {
			players := map[string]any{"level1": map[string]any{"default_level": level}, "worker": testWorkerPlayer(behaviour), "sender": testWorkerPlayer("sends")}
			matches := []map[string]any{
				{"id": "first", "map": "duel", "seats": []string{"worker", "level1"}},
				{"id": "second", "map": "duel", "seats": []string{"level1", "worker"}},
				{"id": "both", "map": "duel", "seats": []string{"worker", "worker"}},
				{"id": "other", "map": "duel", "seats": []string{"sender", "level1"}},
			}
			// One match at a time, so each match gets the processes the
			// one before it gave back.
			lines, _ := playSchedule(t, players, matches, map[string]any{"max_generations": 8, "parallel": 1, "turn_timeout_milliseconds": 200})
			for id, seats := range map[string][]int{"first": {0}, "second": {1}, "both": {0, 1}} {
				for _, index := range seats {
					seat := lines[id].Seats[index]
					if seat.FailedTurns != 1 || seat.Failures[0].Turn != 3 || !strings.HasPrefix(seat.Failures[0].Message, message) || seat.TurnsPlayed != 7 {
						t.Fatalf("%s seat %d: %+v, want turn 3 alone to fail with %q", id, index, seat, message)
					}
					// The replacement's start is not part of any turn's time.
					if behaviour == "slow" && (seat.SlowestTurn != 3 || seat.SlowestTurnMilliseconds < 200 || seat.SlowestTurnMilliseconds > 400) {
						t.Fatalf("%s seat %d: slowest turn %d took %.0f ms, want turn 3 at the 200 ms timeout", id, index, seat.SlowestTurn, seat.SlowestTurnMilliseconds)
					}
					if seat.Commands == 0 {
						t.Fatalf("%s seat %d sent nothing in its other turns", id, index)
					}
				}
			}
			if other := lines["other"].Seats[0]; other.FailedTurns != 0 {
				t.Fatalf("a player that never failed: %+v", other)
			}
		})
	}
}

func TestRunStartsNoMatchOnceItIsToldToStop(t *testing.T) {
	level := uint(1)
	directory := t.TempDir()
	mapPath := writeFile(t, directory, "duel.json", testMapFile)
	plan, err := json.Marshal(map[string]any{"players": map[string]any{"level1": map[string]any{"default_level": level}}, "maps": map[string]string{"duel": mapPath},
		"matches": []map[string]any{{"id": "a", "map": "duel", "seats": []string{"level1", "level1"}}, {"id": "b", "map": "duel", "seats": []string{"level1", "level1"}}}})
	if err != nil {
		t.Fatal(err)
	}
	stopped, cancel := context.WithCancel(context.Background())
	cancel()
	var output bytes.Buffer
	if err := run(stopped, nil, bytes.NewReader(plan), &output, io.Discard); !errors.Is(err, context.Canceled) || output.Len() != 0 {
		t.Fatalf("err = %v and output %q, want context.Canceled and no line", err, output.String())
	}
}

func TestRunWritesPlainDemosAndNoneWithoutADirectory(t *testing.T) {
	players := map[string]any{"sender": testWorkerPlayer("sends"), "broken": testWorkerPlayer("broken")}
	matches := []map[string]any{{"id": "only", "map": "duel", "seats": []string{"sender", "broken"}}}
	lines, directory := playSchedule(t, players, matches, map[string]any{"max_generations": 30})
	if lines["only"].Demo != filepath.Join(directory, "demos", "only.json") {
		t.Fatalf("demo = %q, want only.json in the demo directory", lines["only"].Demo)
	}
	data, err := os.ReadFile(lines["only"].Demo)
	if err != nil || !bytes.HasPrefix(data, []byte(`{"format":"island-battles-demo"`)) {
		t.Fatalf("the demo is not a plain demo file: %v %.40s", err, data)
	}
	lines, _ = playSchedule(t, players, matches, map[string]any{"max_generations": 30, "demo_directory": ""})
	if lines["only"].Demo != "" || lines["only"].Error != "" {
		t.Fatalf("line = %+v, want a played match without a demo", lines["only"])
	}
}

func TestRunRefusesAScheduleItCannotPlay(t *testing.T) {
	directory := t.TempDir()
	mapPath := filepath.Join(directory, "duel.json")
	if err := os.WriteFile(mapPath, []byte(testMapFile), 0o644); err != nil {
		t.Fatal(err)
	}
	maps := fmt.Sprintf(`"maps": {"duel": %q}`, mapPath)
	for name, plan := range map[string]string{
		"not JSON":               `{`,
		"an unknown setting":     `{"players": {}, "maps": {}, "matches": [], "speed": 2}`,
		"a player of two kinds":  `{"players": {"both": {"default_level": 1, "worker": ["bot"]}}, "matches": []}`,
		"a player of no kind":    `{"players": {"none": {}}, "matches": []}`,
		"an unknown level":       `{"players": {"best": {"default_level": 99}}, "matches": []}`,
		"a missing map file":     `{"maps": {"gone": "` + filepath.Join(directory, "gone.json") + `"}, "matches": []}`,
		"an unknown map":         `{"players": {"level1": {"default_level": 1}}, ` + maps + `, "matches": [{"id": "a", "map": "other", "seats": ["level1", "level1"]}]}`,
		"an unknown player":      `{"players": {"level1": {"default_level": 1}}, ` + maps + `, "matches": [{"id": "a", "map": "duel", "seats": ["level1", "level2"]}]}`,
		"an ID used twice":       `{"players": {"level1": {"default_level": 1}}, ` + maps + `, "matches": [{"id": "a", "map": "duel", "seats": ["level1", "level1"]}, {"id": "a", "map": "duel", "seats": ["level1", "level1"]}]}`,
		"an ID that is a path":   `{"players": {"level1": {"default_level": 1}}, ` + maps + `, "matches": [{"id": "../a", "map": "duel", "seats": ["level1", "level1"]}]}`,
		"a map file with a typo": `{"maps": {"bad": "` + writeFile(t, directory, "bad.json", `{"map_nam": "x"}`) + `"}, "matches": []}`,
	} {
		if err := run(context.Background(), nil, strings.NewReader(plan), io.Discard, io.Discard); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
}

func TestRunReportsAMatchWhoseSeatsDoNotFitItsMap(t *testing.T) {
	level := uint(1)
	lines, _ := playSchedule(t, map[string]any{"level1": map[string]any{"default_level": level}},
		[]map[string]any{{"id": "three", "map": "duel", "seats": []string{"level1", "level1", "level1"}}}, nil)
	if !strings.Contains(lines["three"].Error, "is for 2 teams") || lines["three"].Outcome != nil {
		t.Fatalf("line = %+v, want an error that names the map's teams", lines["three"])
	}
}

func TestRunReportsAWorkerThatDoesNotStart(t *testing.T) {
	level := uint(1)
	players := map[string]any{"level1": map[string]any{"default_level": level}, "missing": map[string]any{"worker": []string{filepath.Join(t.TempDir(), "no-such-bot")}}}
	lines, _ := playSchedule(t, players, []map[string]any{{"id": "a", "map": "duel", "seats": []string{"missing", "level1"}}}, nil)
	if !strings.Contains(lines["a"].Error, "start the worker of missing") {
		t.Fatalf("line = %+v, want an error that names the worker", lines["a"])
	}
}

func writeFile(t *testing.T, directory, name, content string) string {
	t.Helper()
	path := filepath.Join(directory, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}
