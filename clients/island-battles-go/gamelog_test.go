package islandbattles

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"testing"
	"unicode/utf8"
)

func lines(count int, prefix string) []LogLine {
	made := []LogLine{}
	for index := range count {
		made = append(made, Log(fmt.Sprintf("%s %d", prefix, index+1)))
	}
	return made
}

func TestGameLogKeepsTheLimitAndCountsWhatItDrops(t *testing.T) {
	for _, test := range []struct {
		name        string
		stored      int
		added       int
		wantLines   int
		wantDropped int
	}{
		{"105 lines: 99 and the error", 0, 105, 100, 6},
		{"exactly 100 lines: the held line is sent", 0, 100, 100, 0},
		{"61 stored, 50 more: 38 and the error", 61, 50, 39, 12},
		{"100 stored: nothing", 100, 3, 0, 0},
		{"99 stored, 2 more: the error alone", 99, 2, 1, 2},
	} {
		t.Run(test.name, func(t *testing.T) {
			log := newGameLog(9, test.stored)
			first := 0
			for _, line := range lines(test.added, "line") {
				if log.add(line) {
					first++
				}
			}
			sent, _ := log.batch(true)
			if len(sent) != test.wantLines {
				t.Fatalf("sent %d lines", len(sent))
			}
			if test.wantDropped > 0 {
				want := LogLine{Level: LogError, Logger: SDKLogger, Message: fmt.Sprintf("game log limit reached for generation 9: 100 lines per generation; %d lines were not stored", test.wantDropped)}
				if sent[len(sent)-1] != want || first != 1 {
					t.Fatalf("got %v, first drop reported %d times", sent[len(sent)-1], first)
				}
			} else if first != 0 && test.stored < MaxLogLines {
				t.Fatalf("reported a drop %d times", first)
			}
		})
	}
}

func TestGameLogBatchesUntilTheLastRequest(t *testing.T) {
	log := newGameLog(1, 0)
	for _, line := range lines(3, "early") {
		log.add(line)
	}
	sent, count := log.batch(false)
	if len(sent) != 3 || count != 3 {
		t.Fatalf("got %d lines, count %d", len(sent), count)
	}
	// A rejected request leaves the lines for the next one.
	if again, _ := log.batch(false); len(again) != 3 {
		t.Fatalf("got %d lines", len(again))
	}
	log.commit(count, false)
	log.add(Log("late"))
	if sent, count := log.batch(true); len(sent) != 1 || count != 1 || sent[0].Message != "late" {
		t.Fatalf("got %v, count %d", sent, count)
	}
	log.commit(1, true)
	log.add(Log("after the last request"))
	if sent, _ := log.batch(true); len(sent) != 0 {
		t.Fatalf("got %v", sent)
	}
}

func TestLoggedLinesAreCutAndMadeStorable(t *testing.T) {
	var output bytes.Buffer
	turn := newTurnLog(newGameLog(4, 0), &logOutputs{level: slog.LevelInfo, writers: []io.Writer{&output}})
	logger := Logger(turn.context(context.Background()))
	logger.Info("a NUL \x00 and invalid UTF-8 \xff")
	logger.Info(strings.Repeat("y", 600))
	logger.Info("")
	sent, _ := turn.gameLog.batch(true)
	if len(sent) != 2 || sent[0].Message != "a NUL � and invalid UTF-8 �" {
		t.Fatalf("got %v", sent)
	}
	if message := sent[1].Message; utf8.RuneCountInString(message) != 500 || message != strings.Repeat("y", 500-utf8.RuneCountInString(cutNote))+"… (cut at 500 characters)" {
		t.Fatalf("got %q", message)
	}
	// The console gets the whole line.
	if !strings.Contains(output.String(), strings.Repeat("y", 600)) {
		t.Fatal("the console line was cut")
	}
}

func TestTheLimitErrorIsWrittenOnceWhenTheFirstLineIsDropped(t *testing.T) {
	var output bytes.Buffer
	turn := newTurnLog(newGameLog(4, 0), &logOutputs{level: slog.LevelInfo, writers: []io.Writer{&output}})
	logger := Logger(turn.context(context.Background()))
	for index := range 105 {
		logger.Info(fmt.Sprintf("line %d", index+1))
	}
	if count := strings.Count(output.String(), "game log limit reached for generation 4: 100 lines per generation; the rest of this generation's lines are not stored"); count != 1 {
		t.Fatalf("written %d times:\n%s", count, output.String())
	}
	if !strings.Contains(output.String(), "line 105") {
		t.Fatal("the console missed a line")
	}
}

func TestLoggerOutsideATurnIsTheDefault(t *testing.T) {
	if Logger(context.Background()) != slog.Default() {
		t.Fatal("not the default logger")
	}
}
