package native

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"islandBattles/backend/botpackage"
)

// sdkDirectory is the Go SDK's module, whose package command and examples
// the end-to-end test uses.
const sdkDirectory = "../../../clients/island-battles-go"

// TestPlaysAPackagedGoExampleEndToEnd packages the SDK's level 1 example
// with the SDK's own package command (#216), and plays a turn of it with
// the runner, as on Lambda: it downloads the zip, unpacks it, starts
// `./bot worker` and submits the bot's answer with the token.
//
// The package command builds for Linux arm64, the Lambda's architecture.
// On any other machine, such as CI's amd64 runners, the test builds the
// same example for this machine and puts it in the zip in place of the
// arm64 binary, keeping the package's manifest and layout.
func TestPlaysAPackagedGoExampleEndToEnd(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the example twice")
	}
	work := t.TempDir()
	packaged := filepath.Join(work, "island-battles-bot.zip")
	goCommand(t, "run", "./cmd/island-battles-package", "--output", packaged, "./examples/level1")
	zipData, err := os.ReadFile(packaged)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "linux" || runtime.GOARCH != "arm64" {
		hostBinary := filepath.Join(work, "bot")
		goCommand(t, "build", "-trimpath", "-o", hostBinary, "./examples/level1")
		zipData = replaceEntry(t, zipData, "bot", hostBinary)
	}

	fake, served := serveGame(t), servePackages(t)
	runner := newRunner(t, fake)
	event := exampleEvent(t, served.add("level1", zipData), fake.server.URL+"/generation/commands", fastLimits)
	outcome := mustHandle(t, runner, event)

	if outcome != (Outcome{Outcome: "submitted", Status: http.StatusOK}) {
		t.Fatalf("outcome = %+v", outcome)
	}
	taken := fake.taken()
	if len(taken) != 1 {
		t.Fatalf("submissions = %d", len(taken))
	}
	if taken[0].authorization != "Bearer "+event.Delivery.Submit.Token {
		t.Errorf("Authorization = %q", taken[0].authorization)
	}
	batch := taken[0].batch
	if batch.GameID != 1 || batch.GenerationID != 12 || !batch.Complete {
		t.Errorf("batch = %+v", batch)
	}
	// Level 1 sends one soldier from its first castle with soldiers to the
	// first enemy castle: castle 0 to castle 1 in the example's turn.
	commands, _ := json.Marshal(batch.Commands)
	if string(commands) != `[{"command_type":"send_soldier_group","parameters":{"origin_castle_id":0,"destination_castle_id":1,"soldier_amount":1}}]` {
		t.Errorf("commands = %s", commands)
	}
	if len(batch.Logs) == 0 || !strings.HasPrefix(batch.Logs[0].Logger, "island_battles.bots.level1") {
		t.Errorf("the bot's own log lines must reach the game: %+v", batch.Logs)
	}
}

func goCommand(t *testing.T, arguments ...string) {
	t.Helper()
	command := exec.Command("go", arguments...)
	command.Dir = sdkDirectory
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("go %s: %v\n%s", strings.Join(arguments, " "), err, output)
	}
}

// replaceEntry returns the zip with the named entry's contents taken from
// the file at path, and everything else as it was.
func replaceEntry(t *testing.T, zipData []byte, name, path string) []byte {
	t.Helper()
	archive, err := zip.NewReader(bytes.NewReader(zipData), int64(len(zipData)))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := botpackage.Inspect(bytes.NewReader(zipData), int64(len(zipData))); err != nil {
		t.Fatalf("the package command's zip must pass the upload's check: %v", err)
	}
	var buffer bytes.Buffer
	rewritten := zip.NewWriter(&buffer)
	for _, entry := range archive.File {
		header := entry.FileHeader
		writer, err := rewritten.CreateHeader(&header)
		if err != nil {
			t.Fatal(err)
		}
		var source io.ReadCloser
		if entry.Name == name {
			source, err = os.Open(path)
		} else {
			source, err = entry.Open()
		}
		if err != nil {
			t.Fatal(err)
		}
		if _, err := io.Copy(writer, source); err != nil {
			t.Fatal(err)
		}
		_ = source.Close()
	}
	if err := rewritten.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}
