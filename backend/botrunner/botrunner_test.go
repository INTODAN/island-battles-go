package botrunner

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
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"islandBattles/backend/models"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/lambda"
	"github.com/aws/aws-sdk-go-v2/service/lambda/types"
)

// fakeLambda records the invocations, as Lambda's API would take them.
type fakeLambda struct {
	inputs []*lambda.InvokeInput
	status int32
	err    error
}

func (fake *fakeLambda) Invoke(_ context.Context, input *lambda.InvokeInput, _ ...func(*lambda.Options)) (*lambda.InvokeOutput, error) {
	fake.inputs = append(fake.inputs, input)
	if fake.err != nil {
		return nil, fake.err
	}
	return &lambda.InvokeOutput{StatusCode: fake.status}, nil
}

// exampleEvent is backend/botrunner/testdata/event.json, the example both
// runners' tests use.
func exampleEvent(t *testing.T) models.BotRunnerEvent {
	t.Helper()
	data, err := os.ReadFile("testdata/event.json")
	if err != nil {
		t.Fatal(err)
	}
	var event models.BotRunnerEvent
	if err := json.Unmarshal(data, &event); err != nil {
		t.Fatal(err)
	}
	return event
}

func TestLambdaQueuesTheEventForTheRunnersFunction(t *testing.T) {
	fake := &fakeLambda{status: http.StatusAccepted}
	runners := &Lambda{Client: fake, Functions: map[models.BotRunnerName]string{
		models.BotRunnerPython: "island-battles-bot-runner-python", models.BotRunnerNative: "island-battles-bot-runner-native"}}
	event := exampleEvent(t)
	hosted := exampleEvent(t)
	hosted.Package, hosted.Limits, hosted.Hosted = nil, nil, &models.BotRunnerHosted{URL: "https://bot.example.org/turn"}

	if err := runners.Invoke(context.Background(), models.BotRunnerPython, event); err != nil {
		t.Fatal(err)
	}
	if err := runners.Invoke(context.Background(), models.BotRunnerNative, hosted); err != nil {
		t.Fatal(err)
	}

	if len(fake.inputs) != 2 {
		t.Fatalf("invocations = %d", len(fake.inputs))
	}
	for index, want := range []struct {
		function string
		event    models.BotRunnerEvent
	}{{"island-battles-bot-runner-python", event}, {"island-battles-bot-runner-native", hosted}} {
		input := fake.inputs[index]
		if aws.ToString(input.FunctionName) != want.function || input.InvocationType != types.InvocationTypeEvent {
			t.Errorf("invocation %d: %s, %s", index, aws.ToString(input.FunctionName), input.InvocationType)
		}
		wantPayload, _ := json.Marshal(want.event)
		if !bytes.Equal(input.Payload, wantPayload) {
			t.Errorf("invocation %d: payload %s", index, input.Payload)
		}
	}
}

func TestLambdaReportsWhatDidntQueue(t *testing.T) {
	event := exampleEvent(t)
	for name, fake := range map[string]*fakeLambda{
		"throttled": {err: errors.New("TooManyRequestsException: Rate Exceeded")},
		"not 202":   {status: http.StatusOK},
	} {
		runners := &Lambda{Client: fake, Functions: map[models.BotRunnerName]string{models.BotRunnerPython: "runner"}}
		if err := runners.Invoke(context.Background(), models.BotRunnerPython, event); err == nil {
			t.Errorf("%s: Invoke must fail", name)
		}
	}
	fake := &fakeLambda{status: http.StatusAccepted}
	runners := &Lambda{Client: fake, Functions: map[models.BotRunnerName]string{models.BotRunnerPython: "runner"}}
	if err := runners.Invoke(context.Background(), models.BotRunnerNative, event); err == nil || len(fake.inputs) != 0 {
		t.Errorf("a runner without a function: %v", err)
	}
}

// fakeGame takes the runners' submissions.
type fakeGame struct {
	server *httptest.Server
	mutex  sync.Mutex
	taken  []map[string]any
	done   chan struct{}
}

func newFakeGame(t *testing.T, expected int) *fakeGame {
	game := &fakeGame{done: make(chan struct{})}
	game.server = httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		var batch map[string]any
		_ = json.NewDecoder(request.Body).Decode(&batch)
		game.mutex.Lock()
		game.taken = append(game.taken, batch)
		if len(game.taken) == expected {
			close(game.done)
		}
		game.mutex.Unlock()
		response.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(game.server.Close)
	return game
}

func (game *fakeGame) wait(t *testing.T) []map[string]any {
	t.Helper()
	select {
	case <-game.done:
	case <-time.After(20 * time.Second):
		t.Fatal("the runner submitted nothing")
	}
	game.mutex.Lock()
	defer game.mutex.Unlock()
	return game.taken
}

func newLocal(t *testing.T, game *fakeGame) *Local {
	parsed, _ := url.Parse(game.server.URL)
	local := &Local{SubmitHost: parsed.Host, Python: []string{"python3.14"}, Directory: t.TempDir() + "/runners",
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	t.Cleanup(local.Close)
	return local
}

func serveZip(t *testing.T, files map[string]string) string {
	t.Helper()
	var buffer bytes.Buffer
	archive := zip.NewWriter(&buffer)
	for name, data := range files {
		header := &zip.FileHeader{Name: name, Method: zip.Deflate}
		header.SetMode(0o755)
		writer, err := archive.CreateHeader(header)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = writer.Write([]byte(data))
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) { _, _ = response.Write(buffer.Bytes()) }))
	t.Cleanup(server.Close)
	return server.URL + "/package.zip"
}

// answeringBot answers each turn with one command, and its process ID as its
// memory, so a test can tell whether the same worker played.
const answeringBot = `#!/bin/sh
echo '{"ready": true}'
while IFS= read -r line; do
  echo '{"result": {"commands": [], "logs": [], "memory": {"pid": '$$'}}}'
done
`

func TestLocalPlaysANativePackageInProcessAndKeepsItLoaded(t *testing.T) {
	game := newFakeGame(t, 2)
	local := newLocal(t, game)
	event := exampleEvent(t)
	event.Delivery.Submit.URL = game.server.URL + "/generation/commands"
	event.Package = &models.BotRunnerPackage{ID: "native-one", URL: serveZip(t, map[string]string{
		"island-battles-bot.json": `{"format": 1, "language": "native", "command": ["./bot", "worker"]}`, "bot": answeringBot})}

	// Two turns of the same package at once: they play one after the other,
	// on the same worker, as on one Lambda instance.
	for range 2 {
		if err := local.Invoke(context.Background(), models.BotRunnerNative, event); err != nil {
			t.Fatal(err)
		}
	}

	taken := game.wait(t)
	first, second := taken[0]["memory"].(map[string]any)["pid"], taken[1]["memory"].(map[string]any)["pid"]
	if first == nil || first != second {
		t.Errorf("workers %v and %v; want the same one", first, second)
	}
}

func TestLocalDeliversAHostedBotsTurnOnThisMachine(t *testing.T) {
	game := newFakeGame(t, 1)
	local := newLocal(t, game)
	received := make(chan models.TurnDelivery, 1)
	bot := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		var delivery models.TurnDelivery
		_ = json.NewDecoder(request.Body).Decode(&delivery)
		received <- delivery
		response.WriteHeader(http.StatusAccepted)
	}))
	t.Cleanup(bot.Close)
	event := exampleEvent(t)
	event.Package, event.Limits = nil, nil
	event.Hosted = &models.BotRunnerHosted{URL: bot.URL + "/turn"}
	event.Delivery.Submit.URL = game.server.URL + "/generation/commands"

	if err := local.Invoke(context.Background(), models.BotRunnerNative, event); err != nil {
		t.Fatal(err)
	}

	select {
	case delivery := <-received:
		if delivery.Submit.Token != event.Delivery.Submit.Token || delivery.Turn.Generation.ID != 12 {
			t.Errorf("delivered %+v", delivery.Submit)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the hosted bot got nothing")
	}
}

func TestLocalRefusesEventsItCantPlay(t *testing.T) {
	local := newLocal(t, newFakeGame(t, 1))
	hosted := exampleEvent(t)
	hosted.Package, hosted.Hosted = nil, &models.BotRunnerHosted{URL: "http://127.0.0.1:1/turn"}
	traversal := exampleEvent(t)
	traversal.Package.ID = "../escape"
	for name, refused := range map[string]struct {
		runner models.BotRunnerName
		event  models.BotRunnerEvent
	}{
		"hosted on the Python runner": {models.BotRunnerPython, hosted},
		"a package ID that's a path":  {models.BotRunnerNative, traversal},
		"an unknown runner":           {"java", exampleEvent(t)},
	} {
		if err := local.Invoke(context.Background(), refused.runner, refused.event); err == nil {
			t.Errorf("%s: Invoke must refuse", name)
		}
	}
	local.Close()
	if err := local.Invoke(context.Background(), models.BotRunnerNative, hosted); err == nil || !strings.Contains(err.Error(), "stopped") {
		t.Errorf("after Close: %v", err)
	}
}

// fakeWorker speaks the worker protocol of #216, as the SDK's worker does.
const fakeWorker = `import json, os, sys
print(json.dumps({"ready": True}), flush=True)
for line in sys.stdin:
    print(json.dumps({"result": {"commands": [], "logs": [], "memory": {"pid": os.getpid()}}}), flush=True)
`

func TestLocalPlaysAPythonPackageWithRunnerPy(t *testing.T) {
	if _, err := exec.LookPath("python3.14"); err != nil {
		t.Skip("needs python3.14 on PATH")
	}
	game := newFakeGame(t, 2)
	local := newLocal(t, game)
	event := exampleEvent(t)
	event.Delivery.Submit.URL = game.server.URL + "/generation/commands"
	event.Package = &models.BotRunnerPackage{ID: "python-one", URL: serveZip(t, map[string]string{
		"island-battles-bot.json":                `{"format": 1, "language": "python", "python": "3.14", "entry": "bot:play"}`,
		"site/island_battles_client/__init__.py": "",
		"site/island_battles_client/worker.py":   fakeWorker,
		"site/bot.py":                            "def play(turn): ...\n"})}

	for range 2 {
		if err := local.Invoke(context.Background(), models.BotRunnerPython, event); err != nil {
			t.Fatal(err)
		}
	}

	taken := game.wait(t)
	first, second := taken[0]["memory"], taken[1]["memory"]
	if first == nil || first.(map[string]any)["pid"] != second.(map[string]any)["pid"] {
		t.Errorf("memories %v and %v; want the same worker", first, second)
	}
}
