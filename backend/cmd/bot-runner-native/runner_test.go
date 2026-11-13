package main

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"islandBattles/backend/botpackage"
	"islandBattles/backend/models"
)

// The fake packages are shell scripts that speak the worker protocol, under
// the native manifest that the Go SDK's package command writes.
const nativeManifest = `{"format": 1, "language": "native", "command": ["./bot", "worker"]}`

// answeringBot answers every turn with one command, and its process ID as
// its memory, so a test can tell whether the same worker played.
const answeringBot = `#!/bin/sh
echo "pid=$$ starting" >&2
echo '{"ready": true}'
while IFS= read -r line; do
  echo '{"result": {"commands": [{"command_type": "send_soldier_group", "parameters": {"origin_castle_id": 0, "destination_castle_id": 1, "soldier_amount": 1}}], "logs": [{"level": "info", "logger": "island_battles.player", "message": "sent one"}], "memory": {"pid": '$$'}}}'
done
`

type zipEntry struct {
	name string
	mode os.FileMode
	data string
}

func packageZip(t *testing.T, entries ...zipEntry) []byte {
	t.Helper()
	var buffer bytes.Buffer
	archive := zip.NewWriter(&buffer)
	for _, entry := range entries {
		header := &zip.FileHeader{Name: entry.name, Method: zip.Deflate}
		header.SetMode(entry.mode)
		writer, err := archive.CreateHeader(header)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := writer.Write([]byte(entry.data)); err != nil {
			t.Fatal(err)
		}
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

func botPackage(t *testing.T, script string) []byte {
	return packageZip(t, zipEntry{botpackage.ManifestName, 0o644, nativeManifest}, zipEntry{"bot", 0o755, script})
}

// packages serves zips by path and counts their downloads.
type packages struct {
	server    *httptest.Server
	mutex     sync.Mutex
	zips      map[string][]byte
	downloads map[string]int
}

func servePackages(t *testing.T) *packages {
	served := &packages{zips: map[string][]byte{}, downloads: map[string]int{}}
	served.server = httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		served.mutex.Lock()
		data, found := served.zips[request.URL.Path]
		served.downloads[request.URL.Path]++
		served.mutex.Unlock()
		if !found {
			http.NotFound(response, request)
			return
		}
		_, _ = response.Write(data)
	}))
	t.Cleanup(served.server.Close)
	return served
}

func (served *packages) add(id string, data []byte) models.BotRunnerPackage {
	served.mutex.Lock()
	defer served.mutex.Unlock()
	served.zips["/"+id+".zip"] = data
	return models.BotRunnerPackage{ID: id, URL: served.server.URL + "/" + id + ".zip", SizeBytes: int64(len(data))}
}

func (served *packages) downloadsOf(id string) int {
	served.mutex.Lock()
	defer served.mutex.Unlock()
	return served.downloads["/"+id+".zip"]
}

// submission is one batch the fake game took.
type submission struct {
	authorization string
	userAgent     string
	batch         struct {
		GameID       uint                       `json:"game_id"`
		GenerationID uint                       `json:"generation_id"`
		RequestID    string                     `json:"request_id"`
		Complete     bool                       `json:"complete"`
		Commands     []json.RawMessage          `json:"commands"`
		Logs         []models.GenerationLogLine `json:"logs"`
		Memory       json.RawMessage            `json:"memory"`
	}
}

// game is a fake game that takes POST /generation/commands.
type game struct {
	server      *httptest.Server
	mutex       sync.Mutex
	submissions []submission
	// answer is the status of the next answers; 0 means 200.
	answers []int
}

func serveGame(t *testing.T) *game {
	fake := &game{}
	fake.server = httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost || request.URL.Path != "/generation/commands" {
			http.NotFound(response, request)
			return
		}
		var taken submission
		taken.authorization = request.Header.Get("Authorization")
		taken.userAgent = request.Header.Get("User-Agent")
		decoder := json.NewDecoder(request.Body)
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&taken.batch); err != nil {
			http.Error(response, err.Error(), http.StatusBadRequest)
			return
		}
		fake.mutex.Lock()
		fake.submissions = append(fake.submissions, taken)
		status := http.StatusOK
		if len(fake.answers) > 0 {
			status, fake.answers = fake.answers[0], fake.answers[1:]
		}
		fake.mutex.Unlock()
		response.Header().Set("Content-Type", "application/problem+json")
		response.WriteHeader(status)
		if status == http.StatusUnprocessableEntity {
			_, _ = response.Write([]byte(`{"type": "about:blank", "detail": "castle 0 has no soldiers to send"}`))
		}
	}))
	t.Cleanup(fake.server.Close)
	return fake
}

func (fake *game) taken() []submission {
	fake.mutex.Lock()
	defer fake.mutex.Unlock()
	return append([]submission(nil), fake.submissions...)
}

func (fake *game) host() string {
	parsed, _ := url.Parse(fake.server.URL)
	return parsed.Host
}

// exampleEvent is backend/botrunner/testdata/event.json, the example both
// runners' tests use, with the package and the submit URL given.
func exampleEvent(t *testing.T, botPackage models.BotRunnerPackage, submitURL string, limits models.BotRunnerLimits) models.BotRunnerEvent {
	t.Helper()
	data, err := os.ReadFile("../../botrunner/testdata/event.json")
	if err != nil {
		t.Fatal(err)
	}
	var event models.BotRunnerEvent
	if err := json.Unmarshal(data, &event); err != nil {
		t.Fatal(err)
	}
	event.Package = &botPackage
	event.Limits = &limits
	event.Delivery.Submit.URL = submitURL
	return event
}

var fastLimits = models.BotRunnerLimits{LoadSeconds: 5, PlaySeconds: 2}

func newRunner(t *testing.T, fake *game) *Runner {
	runner := &Runner{Directory: filepath.Join(t.TempDir(), "bot"), SubmitHost: fake.host(), AllowHTTPPackages: true,
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	t.Cleanup(runner.Unload)
	return runner
}

func handle(t *testing.T, runner *Runner, event models.BotRunnerEvent) (Outcome, error) {
	t.Helper()
	payload, err := json.Marshal(event)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	return runner.Handle(ctx, payload)
}

func mustHandle(t *testing.T, runner *Runner, event models.BotRunnerEvent) Outcome {
	t.Helper()
	outcome, err := handle(t, runner, event)
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	return outcome
}

// passLine checks that the submission is a pass, as the default bots pass,
// and returns its log line.
func passLine(t *testing.T, taken submission) string {
	t.Helper()
	if len(taken.batch.Commands) != 0 || !taken.batch.Complete {
		t.Fatalf("a pass sends no commands and completes the turn: %+v", taken.batch)
	}
	if string(taken.batch.Memory) != `{"sent_total":4}` {
		t.Errorf("a pass keeps the turn's memory, got %s", taken.batch.Memory)
	}
	if len(taken.batch.Logs) != 1 || taken.batch.Logs[0].Level != models.GenerationLogError || taken.batch.Logs[0].Logger != "island_battles.bots" {
		t.Fatalf("a pass logs one error line from island_battles.bots: %+v", taken.batch.Logs)
	}
	message := taken.batch.Logs[0].Message
	if length := len([]rune(message)); length == 0 || length > models.MaxGenerationLogMessageLength {
		t.Errorf("the line has %d characters", length)
	}
	return message
}

func memoryPID(t *testing.T, taken submission) int {
	t.Helper()
	var memory struct {
		PID int `json:"pid"`
	}
	if err := json.Unmarshal(taken.batch.Memory, &memory); err != nil || memory.PID == 0 {
		t.Fatalf("the memory isn't the fake bot's: %s", taken.batch.Memory)
	}
	return memory.PID
}

func TestPlaysAndSubmitsTheAnswerWithTheToken(t *testing.T) {
	fake, served := serveGame(t), servePackages(t)
	runner := newRunner(t, fake)
	event := exampleEvent(t, served.add("one", botPackage(t, answeringBot)), fake.server.URL+"/generation/commands", fastLimits)

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
	if batch.GameID != event.Delivery.Turn.GameID || batch.GenerationID != event.Delivery.Turn.Generation.ID || !batch.Complete || batch.RequestID == "" {
		t.Errorf("batch = %+v", batch)
	}
	if len(batch.Commands) != 1 || len(batch.Logs) != 1 || batch.Logs[0].Message != "sent one" {
		t.Errorf("the batch must carry the bot's commands and logs: %+v", batch)
	}
}

func TestLoadsOnceAndReusesTheSamePackage(t *testing.T) {
	fake, served := serveGame(t), servePackages(t)
	runner := newRunner(t, fake)
	event := exampleEvent(t, served.add("one", botPackage(t, answeringBot)), fake.server.URL+"/generation/commands", fastLimits)

	mustHandle(t, runner, event)
	mustHandle(t, runner, event)

	taken := fake.taken()
	if first, second := memoryPID(t, taken[0]), memoryPID(t, taken[1]); first != second {
		t.Errorf("the same package must keep its worker: pids %d and %d", first, second)
	}
	if downloads := served.downloadsOf("one"); downloads != 1 {
		t.Errorf("downloads = %d, want 1", downloads)
	}
}

// A runtime that timed out leaves its package in /tmp, which the next
// runtime of the instance finds there.
func TestLoadsOverAPackageAnEarlierRuntimeLeft(t *testing.T) {
	fake, served := serveGame(t), servePackages(t)
	runner := newRunner(t, fake)
	if err := os.MkdirAll(filepath.Join(runner.Directory, "one"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(runner.Directory, "one", "bot"), []byte("stale"), 0o755); err != nil {
		t.Fatal(err)
	}

	outcome := mustHandle(t, runner, exampleEvent(t, served.add("one", botPackage(t, answeringBot)), fake.server.URL+"/generation/commands", fastLimits))

	if outcome.Outcome != "submitted" {
		t.Errorf("outcome = %+v", outcome)
	}
}

func TestSwitchingPackagesReloads(t *testing.T) {
	fake, served := serveGame(t), servePackages(t)
	runner := newRunner(t, fake)
	submitURL := fake.server.URL + "/generation/commands"
	one := served.add("one", botPackage(t, answeringBot))
	two := served.add("two", botPackage(t, answeringBot))

	mustHandle(t, runner, exampleEvent(t, one, submitURL, fastLimits))
	firstPID := memoryPID(t, fake.taken()[0])
	mustHandle(t, runner, exampleEvent(t, two, submitURL, fastLimits))
	mustHandle(t, runner, exampleEvent(t, one, submitURL, fastLimits))

	taken := fake.taken()
	if memoryPID(t, taken[1]) == firstPID || memoryPID(t, taken[2]) == memoryPID(t, taken[1]) {
		t.Error("another package must start another worker")
	}
	if !errors.Is(syscall.Kill(firstPID, 0), syscall.ESRCH) {
		t.Error("the first package's worker must be stopped")
	}
	if served.downloadsOf("one") != 2 || served.downloadsOf("two") != 1 {
		t.Errorf("downloads: one %d, two %d", served.downloadsOf("one"), served.downloadsOf("two"))
	}
	entries, err := os.ReadDir(runner.Directory)
	if err != nil || len(entries) != 1 || entries[0].Name() != "one" {
		t.Errorf("the runner keeps only the loaded package: %v %v", entries, err)
	}
}

func TestALoadTimeoutPasses(t *testing.T) {
	fake, served := serveGame(t), servePackages(t)
	runner := newRunner(t, fake)
	slow := "#!/bin/sh\necho 'importing a lot' >&2\nsleep 10\necho '{\"ready\": true}'\n"
	event := exampleEvent(t, served.add("slow", botPackage(t, slow)), fake.server.URL+"/generation/commands",
		models.BotRunnerLimits{LoadSeconds: 0.5, PlaySeconds: 2})

	started := time.Now()
	outcome := mustHandle(t, runner, event)

	if elapsed := time.Since(started); elapsed > 5*time.Second {
		t.Errorf("the load took %v", elapsed)
	}
	if outcome.Outcome != "passed" {
		t.Fatalf("outcome = %+v", outcome)
	}
	message := passLine(t, fake.taken()[0])
	if !strings.Contains(message, "could not be loaded") || !strings.Contains(message, "not ready within") || !strings.Contains(message, "importing a lot") {
		t.Errorf("message = %q", message)
	}
	if runner.loaded != nil {
		t.Error("a bot that didn't load must not stay loaded")
	}
}

func TestAPlayTimeoutPassesAndKillsTheWorker(t *testing.T) {
	fake, served := serveGame(t), servePackages(t)
	runner := newRunner(t, fake)
	slow := "#!/bin/sh\necho \"pid=$$\" >&2\necho '{\"ready\": true}'\nread -r line\nsleep 10\n"
	event := exampleEvent(t, served.add("slow", botPackage(t, slow)), fake.server.URL+"/generation/commands",
		models.BotRunnerLimits{LoadSeconds: 5, PlaySeconds: 0.3})

	outcome := mustHandle(t, runner, event)

	if outcome.Outcome != "passed" {
		t.Fatalf("outcome = %+v", outcome)
	}
	message := passLine(t, fake.taken()[0])
	if !strings.Contains(message, "did not answer within 300ms") {
		t.Errorf("message = %q", message)
	}
	found := regexp.MustCompile(`pid=(\d+)`).FindStringSubmatch(message)
	if found == nil {
		t.Fatalf("the line must hold the worker's stderr: %q", message)
	}
	pid, _ := strconv.Atoi(found[1])
	if !errors.Is(syscall.Kill(pid, 0), syscall.ESRCH) {
		t.Errorf("the worker %d must be killed", pid)
	}
	if runner.loaded != nil {
		t.Error("a worker that timed out must be loaded again next turn")
	}
	// The next turn loads the package again.
	mustHandle(t, runner, event)
	if served.downloadsOf("slow") != 2 {
		t.Errorf("downloads = %d, want 2", served.downloadsOf("slow"))
	}
}

func TestACrashIncludesStderrInTheLogLine(t *testing.T) {
	fake, served := serveGame(t), servePackages(t)
	runner := newRunner(t, fake)
	crashing := "#!/bin/sh\necho '{\"ready\": true}'\nread -r line\necho 'panic: runtime error: index out of range [3] with length 3' >&2\nexit 2\n"
	event := exampleEvent(t, served.add("crash", botPackage(t, crashing)), fake.server.URL+"/generation/commands", fastLimits)

	mustHandle(t, runner, event)

	message := passLine(t, fake.taken()[0])
	if !strings.Contains(message, "exit status 2") || !strings.Contains(message, "index out of range [3] with length 3") {
		t.Errorf("message = %q", message)
	}
	if runner.loaded != nil {
		t.Error("a crashed worker must be loaded again next turn")
	}
}

func TestALongStderrIsCutToTheLineLimit(t *testing.T) {
	fake, served := serveGame(t), servePackages(t)
	runner := newRunner(t, fake)
	crashing := "#!/bin/sh\necho '{\"ready\": true}'\nread -r line\ni=0\nwhile [ $i -lt 200 ]; do echo \"line $i of the bot's output\" >&2; i=$((i+1)); done\nexit 1\n"
	event := exampleEvent(t, served.add("crash", botPackage(t, crashing)), fake.server.URL+"/generation/commands", fastLimits)

	mustHandle(t, runner, event)

	message := passLine(t, fake.taken()[0])
	if len([]rune(message)) != models.MaxGenerationLogMessageLength || !strings.HasSuffix(message, "line 199 of the bot's output") {
		t.Errorf("the line must keep the last output, cut to 500 characters: %q", message)
	}
}

func TestAnErrorAnswerPassesAndKeepsTheWorker(t *testing.T) {
	fake, served := serveGame(t), servePackages(t)
	runner := newRunner(t, fake)
	failing := "#!/bin/sh\necho '{\"ready\": true}'\nwhile IFS= read -r line; do echo '{\"error\": \"ValueError: no castles\"}'; done\n"
	event := exampleEvent(t, served.add("failing", botPackage(t, failing)), fake.server.URL+"/generation/commands", fastLimits)

	mustHandle(t, runner, event)
	mustHandle(t, runner, event)

	message := passLine(t, fake.taken()[0])
	if message != "The uploaded bot failed this turn, so it passes: ValueError: no castles" {
		t.Errorf("message = %q", message)
	}
	if served.downloadsOf("failing") != 1 {
		t.Error("a bot that answered with an error keeps its worker")
	}
}

func TestARejectedAnswerPasses(t *testing.T) {
	fake, served := serveGame(t), servePackages(t)
	fake.answers = []int{http.StatusUnprocessableEntity}
	runner := newRunner(t, fake)
	event := exampleEvent(t, served.add("one", botPackage(t, answeringBot)), fake.server.URL+"/generation/commands", fastLimits)

	outcome := mustHandle(t, runner, event)

	taken := fake.taken()
	if len(taken) != 2 || outcome.Outcome != "passed" {
		t.Fatalf("outcome %+v after %d submissions", outcome, len(taken))
	}
	if message := passLine(t, taken[1]); !strings.Contains(message, "castle 0 has no soldiers to send") {
		t.Errorf("message = %q", message)
	}
}

func TestRetriesAServerError(t *testing.T) {
	fake, served := serveGame(t), servePackages(t)
	fake.answers = []int{http.StatusBadGateway}
	runner := newRunner(t, fake)
	event := exampleEvent(t, served.add("one", botPackage(t, answeringBot)), fake.server.URL+"/generation/commands", fastLimits)

	outcome := mustHandle(t, runner, event)

	taken := fake.taken()
	if outcome.Outcome != "submitted" || len(taken) != 2 || taken[0].batch.RequestID != taken[1].batch.RequestID {
		t.Errorf("a retry sends the same batch again: %+v, %d submissions", outcome, len(taken))
	}
}

func TestRefusesPathsThatLeaveThePackage(t *testing.T) {
	for name, entry := range map[string]zipEntry{
		"parent":        {"../escaped", 0o644, "x"},
		"nested parent": {"site/../../escaped", 0o644, "x"},
		"absolute":      {"/tmp/escaped", 0o644, "x"},
		"backslash":     {`..\escaped`, 0o644, "x"},
		"symbolic link": {"link", os.ModeSymlink | 0o777, "/etc/passwd"},
	} {
		t.Run(name, func(t *testing.T) {
			fake, served := serveGame(t), servePackages(t)
			runner := newRunner(t, fake)
			zipData := packageZip(t, zipEntry{botpackage.ManifestName, 0o644, nativeManifest}, zipEntry{"bot", 0o755, answeringBot}, entry)
			event := exampleEvent(t, served.add("bad", zipData), fake.server.URL+"/generation/commands", fastLimits)

			mustHandle(t, runner, event)

			if message := passLine(t, fake.taken()[0]); !strings.Contains(message, "the package is refused") {
				t.Errorf("message = %q", message)
			}
			if _, err := os.Stat(filepath.Join(runner.Directory, "escaped")); !errors.Is(err, os.ErrNotExist) {
				t.Error("nothing may be unpacked")
			}
		})
	}
}

func TestRefusesAPythonPackage(t *testing.T) {
	fake, served := serveGame(t), servePackages(t)
	runner := newRunner(t, fake)
	zipData := packageZip(t, zipEntry{botpackage.ManifestName, 0o644, `{"format": 1, "language": "python", "python": "3.14", "entry": "bot:play"}`},
		zipEntry{"site/bot.py", 0o644, "def play(turn): ...\n"})
	event := exampleEvent(t, served.add("python", zipData), fake.server.URL+"/generation/commands", fastLimits)

	mustHandle(t, runner, event)

	if message := passLine(t, fake.taken()[0]); !strings.Contains(message, "the Python runner plays") {
		t.Errorf("message = %q", message)
	}
}

func TestRefusesAnOversizedZipBeforeDownloadingIt(t *testing.T) {
	fake, served := serveGame(t), servePackages(t)
	runner := newRunner(t, fake)
	botPackage := served.add("big", botPackage(t, answeringBot))
	botPackage.SizeBytes = botpackage.MaxZipBytes + 1

	mustHandle(t, runner, exampleEvent(t, botPackage, fake.server.URL+"/generation/commands", fastLimits))

	if message := passLine(t, fake.taken()[0]); !strings.Contains(message, "the limit is 262144000") {
		t.Errorf("message = %q", message)
	}
	if served.downloadsOf("big") != 0 {
		t.Error("an oversized package must not be downloaded")
	}
}

func TestRefusesADownloadOverTheLimit(t *testing.T) {
	fake := serveGame(t)
	runner := newRunner(t, fake)
	huge := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.Header().Set("Content-Length", strconv.Itoa(botpackage.MaxZipBytes+1))
		response.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(huge.Close)

	mustHandle(t, runner, exampleEvent(t, models.BotRunnerPackage{ID: "huge", URL: huge.URL + "/huge.zip"}, fake.server.URL+"/generation/commands", fastLimits))

	if message := passLine(t, fake.taken()[0]); !strings.Contains(message, "the limit is 262144000") {
		t.Errorf("message = %q", message)
	}
}

func TestRefusesAZipWhoseSizeDiffersFromTheUpload(t *testing.T) {
	fake, served := serveGame(t), servePackages(t)
	runner := newRunner(t, fake)
	botPackage := served.add("changed", botPackage(t, answeringBot))
	botPackage.SizeBytes--

	mustHandle(t, runner, exampleEvent(t, botPackage, fake.server.URL+"/generation/commands", fastLimits))

	if message := passLine(t, fake.taken()[0]); !strings.Contains(message, "it was uploaded with") {
		t.Errorf("message = %q", message)
	}
}

// A zip whose directory declares more than 1 GiB is refused from its
// directory, without unpacking anything.
func TestRefusesADeclaredUnpackedSizeOverTheLimit(t *testing.T) {
	fake, served := serveGame(t), servePackages(t)
	runner := newRunner(t, fake)
	var buffer bytes.Buffer
	archive := zip.NewWriter(&buffer)
	for _, entry := range []zipEntry{{botpackage.ManifestName, 0o644, nativeManifest}, {"bot", 0o755, answeringBot}} {
		writer, err := archive.Create(entry.name)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = writer.Write([]byte(entry.data))
	}
	header := &zip.FileHeader{Name: "bomb", Method: zip.Store, CompressedSize64: 1, UncompressedSize64: botpackage.MaxUnpackedBytes}
	writer, err := archive.CreateRaw(header)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = writer.Write([]byte("x"))
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}

	mustHandle(t, runner, exampleEvent(t, served.add("bomb", buffer.Bytes()), fake.server.URL+"/generation/commands", fastLimits))

	if message := passLine(t, fake.taken()[0]); !strings.Contains(message, "unpacked, the limit") {
		t.Errorf("message = %q", message)
	}
}

// The limit holds for the bytes actually written, whatever the zip declares.
func TestStopsUnpackingPastTheLimit(t *testing.T) {
	zipData := packageZip(t, zipEntry{"big", 0o644, strings.Repeat("x", 1000)})
	archive, err := zip.NewReader(bytes.NewReader(zipData), int64(len(zipData)))
	if err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()

	written, err := unpackEntry(archive.File[0], directory, 999)

	if err == nil || !strings.Contains(err.Error(), "unpacked, the limit") || written != 1000 {
		t.Errorf("unpackEntry = %d, %v", written, err)
	}
	if _, err := unpackEntry(archive.File[0], t.TempDir(), 1000); err != nil {
		t.Errorf("exactly the room fits: %v", err)
	}
}

func TestSubmitsOnlyToTheConfiguredHost(t *testing.T) {
	fake, served := serveGame(t), servePackages(t)
	elsewhere := serveGame(t)
	botPackage := served.add("one", botPackage(t, answeringBot))
	for name, submitURL := range map[string]string{
		"another host":     elsewhere.server.URL + "/generation/commands",
		"another port":     "http://" + strings.Replace(fake.host(), ":", ":1", 1) + "/generation/commands",
		"user information": strings.Replace(fake.server.URL, "http://", "http://user@", 1) + "/generation/commands",
		"not a URL":        "::",
	} {
		t.Run(name, func(t *testing.T) {
			runner := newRunner(t, fake)
			if _, err := handle(t, runner, exampleEvent(t, botPackage, submitURL, fastLimits)); err == nil || !strings.Contains(err.Error(), "is not on the game's host") {
				t.Errorf("Handle = %v", err)
			}
		})
	}
	if len(fake.taken()) != 0 || len(elsewhere.taken()) != 0 || served.downloadsOf("one") != 0 {
		t.Error("an event with a submit URL elsewhere must do nothing")
	}
}

func TestSubmitURLRules(t *testing.T) {
	for _, accepted := range []struct{ url, host string }{
		{"https://island-battles.example.org/generation/commands", "island-battles.example.org"},
		{"https://Island-Battles.example.org/generation/commands", "island-battles.example.org"},
		{"http://localhost:8888/generation/commands", "localhost:8888"},
		{"http://127.0.0.1:8888/generation/commands", "127.0.0.1:8888"},
	} {
		if err := checkSubmitURL(accepted.url, accepted.host); err != nil {
			t.Errorf("checkSubmitURL(%q, %q) = %v", accepted.url, accepted.host, err)
		}
	}
	for _, refused := range []struct{ url, host string }{
		{"http://island-battles.example.org/generation/commands", "island-battles.example.org"},
		{"https://island-battles.example.org.evil.example/generation/commands", "island-battles.example.org"},
		{"https://island-battles.example.org:8443/generation/commands", "island-battles.example.org"},
		{"https://island-battles.example.org/generation/commands", ""},
	} {
		if err := checkSubmitURL(refused.url, refused.host); err == nil {
			t.Errorf("checkSubmitURL(%q, %q) must refuse", refused.url, refused.host)
		}
	}
}

func TestHostedModePostsTheDelivery(t *testing.T) {
	fake := serveGame(t)
	var received struct {
		body        []byte
		userAgent   string
		contentType string
	}
	hosted := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		received.body, _ = io.ReadAll(request.Body)
		received.userAgent, received.contentType = request.Header.Get("User-Agent"), request.Header.Get("Content-Type")
		response.WriteHeader(http.StatusAccepted)
		_, _ = response.Write([]byte(`{"accepted": true}`))
	}))
	t.Cleanup(hosted.Close)
	runner := newRunner(t, fake)
	event := exampleEvent(t, models.BotRunnerPackage{}, fake.server.URL+"/generation/commands", fastLimits)
	event.Package, event.Limits = nil, nil
	event.Hosted = &models.BotRunnerHosted{URL: hosted.URL + "/turn"}

	outcome := mustHandle(t, runner, event)

	if outcome != (Outcome{Outcome: "delivered", Status: http.StatusAccepted}) {
		t.Errorf("outcome = %+v", outcome)
	}
	if received.userAgent != "island-battles-bot-delivery/1" || received.contentType != "application/json" {
		t.Errorf("User-Agent %q, Content-Type %q", received.userAgent, received.contentType)
	}
	var delivered models.TurnDelivery
	if err := json.Unmarshal(received.body, &delivered); err != nil {
		t.Fatal(err)
	}
	if delivered.Submit.URL != event.Delivery.Submit.URL || delivered.Submit.Token != event.Delivery.Submit.Token || delivered.Turn.GameID != event.Delivery.Turn.GameID || string(delivered.Turn.Memory) != `{"sent_total":4}` {
		t.Errorf("the hosted bot must get the delivery: %s", received.body)
	}
	if len(fake.taken()) != 0 {
		t.Error("the hosted bot submits for itself")
	}
}

func TestHostedModeGivesUpAfterFiveSeconds(t *testing.T) {
	if testing.Short() {
		t.Skip("waits for the delivery's timeout")
	}
	fake := serveGame(t)
	release := make(chan struct{})
	hosted := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { <-release }))
	t.Cleanup(func() { close(release); hosted.Close() })
	runner := newRunner(t, fake)
	event := exampleEvent(t, models.BotRunnerPackage{}, fake.server.URL+"/generation/commands", fastLimits)
	event.Package, event.Limits = nil, nil
	event.Hosted = &models.BotRunnerHosted{URL: hosted.URL + "/turn"}

	started := time.Now()
	outcome := mustHandle(t, runner, event)

	if elapsed := time.Since(started); outcome.Outcome != "undelivered" || elapsed < deliveryTimeout || elapsed > deliveryTimeout+2*time.Second {
		t.Errorf("outcome %+v after %v", outcome, elapsed)
	}
}

func TestRefusesMalformedEvents(t *testing.T) {
	fake := serveGame(t)
	runner := newRunner(t, fake)
	submitURL := fake.server.URL + "/generation/commands"
	both := exampleEvent(t, models.BotRunnerPackage{ID: "one", URL: "https://example.org/one.zip"}, submitURL, fastLimits)
	both.Hosted = &models.BotRunnerHosted{URL: "https://bot.example.org/turn"}
	noLimits := exampleEvent(t, models.BotRunnerPackage{ID: "one", URL: "https://example.org/one.zip"}, submitURL, fastLimits)
	noLimits.Limits = nil
	neither := exampleEvent(t, models.BotRunnerPackage{}, submitURL, fastLimits)
	neither.Package = nil
	for name, event := range map[string]models.BotRunnerEvent{"both": both, "no limits": noLimits, "neither": neither} {
		if _, err := handle(t, runner, event); err == nil {
			t.Errorf("%s: Handle must refuse", name)
		}
	}
	if len(fake.taken()) != 0 {
		t.Error("a malformed event submits nothing")
	}
}
