package main

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	islandbattles "github.com/INTODAN/island-battles-go/clients/island-battles-go"
)

// The demo file format this server reads: the game's Demo schema, as the
// replay's Save demo writes it.
const (
	demoFormat        = "island-battles-demo"
	demoSchemaVersion = 2
	// maxDemoBytes bounds a demo file, unpacked, as the demo viewer does.
	maxDemoBytes = 128 << 20
)

// Demo is a saved game: every board played so far, and every team's bot log
// lines for each.
type Demo struct {
	Format        string                     `json:"format"`
	SchemaVersion int                        `json:"schema_version"`
	SavedAt       time.Time                  `json:"saved_at"`
	GameID        uint                       `json:"game_id"`
	Teams         []DemoTeam                 `json:"teams"`
	Outcome       *DemoOutcome               `json:"outcome"`
	Frames        []islandbattles.Generation `json:"frames"`
	Logs          []GenerationLogs           `json:"logs"`
}

// DemoTeam is a team that played, with its players' display names.
type DemoTeam struct {
	Color   uint     `json:"color"`
	Name    string   `json:"name"`
	Players []string `json:"players"`
}

// DemoOutcome is how the game ended.
type DemoOutcome struct {
	Reason          string `json:"reason"`
	WinnerTeamColor *uint  `json:"winner_team_color"`
	Summary         string `json:"summary"`
}

// GenerationLogs is one board's bot log lines, by player.
type GenerationLogs struct {
	GenerationID uint         `json:"generation_id"`
	Players      []PlayerLogs `json:"players"`
}

// PlayerLogs is one player's lines for a board.
type PlayerLogs struct {
	DisplayName string                  `json:"display_name"`
	TeamColor   uint                    `json:"team_color"`
	Lines       []islandbattles.LogLine `json:"lines"`
}

// readDemoFile reads a demo file from disk, packed with gzip or not.
func readDemoFile(path string) (Demo, error) {
	file, err := os.Open(path)
	if err != nil {
		return Demo{}, err
	}
	defer func() { _ = file.Close() }()
	return readDemo(file)
}

// readDemo decodes a demo, unpacking it first when it starts with gzip's
// magic bytes, and checks what the tools rely on.
func readDemo(reader io.Reader) (Demo, error) {
	buffered := bufio.NewReader(reader)
	var source io.Reader = buffered
	if magic, err := buffered.Peek(2); err == nil && bytes.Equal(magic, []byte{0x1f, 0x8b}) {
		unpacked, err := gzip.NewReader(buffered)
		if err != nil {
			return Demo{}, fmt.Errorf("unpack the demo: %w", err)
		}
		defer func() { _ = unpacked.Close() }()
		source = unpacked
	}
	var demo Demo
	limited := &io.LimitedReader{R: source, N: maxDemoBytes + 1}
	if err := json.NewDecoder(limited).Decode(&demo); err != nil {
		if limited.N <= 0 {
			return Demo{}, fmt.Errorf("the demo is larger than %d MiB", maxDemoBytes>>20)
		}
		return Demo{}, fmt.Errorf("this is not an Island Battles demo file: %w", err)
	}
	return demo, checkDemo(demo)
}

func checkDemo(demo Demo) error {
	if demo.Format != demoFormat {
		return errors.New("this is not an Island Battles demo file: save one with Save demo in a game's replay")
	}
	if demo.SchemaVersion != demoSchemaVersion {
		return fmt.Errorf("this demo has format version %d, and this server reads only version %d: update island-battles-mcp, or save the demo again", demo.SchemaVersion, demoSchemaVersion)
	}
	if len(demo.Frames) == 0 {
		return errors.New("the demo has no turns")
	}
	if len(demo.Logs) != len(demo.Frames) {
		return fmt.Errorf("the demo is damaged: it has %d turns and %d log entries", len(demo.Frames), len(demo.Logs))
	}
	for index := range demo.Frames {
		if demo.Logs[index].GenerationID != demo.Frames[index].ID {
			return fmt.Errorf("the demo is damaged: the logs of turn %d belong to another generation", index+1)
		}
	}
	return nil
}
