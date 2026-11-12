package islandbattles

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"
)

// startBot serves play as serve does, with extra allowed origins.
func startBot(t *testing.T, play Play, turnTimeout time.Duration, extraOrigins ...string) (*httptest.Server, *botServer) {
	t.Helper()
	bot := newBotServer("mybot", play, append(append([]string{}, DefaultOrigins...), extraOrigins...), turnTimeout, io.Discard)
	server := httptest.NewServer(bot)
	t.Cleanup(func() {
		server.Close()
		bot.turns.Wait()
	})
	return server, bot
}

// fakeAPI is a game server's POST /generation/commands that records each
// batch and answers the queued answers, then 200.
type fakeAPI struct {
	server  *httptest.Server
	mutex   sync.Mutex
	batches []submitted
	answers []fakeAnswer
	arrived chan struct{}
}

type submitted struct {
	authorization string
	batch         registerCommands
}

type fakeAnswer struct {
	status int
	body   string
}

func startAPI(t *testing.T, answers ...fakeAnswer) *fakeAPI {
	t.Helper()
	api := &fakeAPI{answers: answers, arrived: make(chan struct{}, 100)}
	api.server = httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		var batch registerCommands
		data, _ := io.ReadAll(request.Body)
		if request.URL.Path != "/generation/commands" || json.Unmarshal(data, &batch) != nil {
			t.Errorf("the bot sent %s %s", request.URL.Path, data)
		}
		api.mutex.Lock()
		api.batches = append(api.batches, submitted{request.Header.Get("Authorization"), batch})
		answer := fakeAnswer{200, "{}"}
		if len(api.answers) > 0 {
			answer, api.answers = api.answers[0], api.answers[1:]
		}
		api.mutex.Unlock()
		response.Header().Set("Content-Type", "application/json")
		response.WriteHeader(answer.status)
		_, _ = io.WriteString(response, answer.body)
		api.arrived <- struct{}{}
	}))
	t.Cleanup(api.server.Close)
	return api
}

func (api *fakeAPI) url() string { return api.server.URL + "/generation/commands" }

// wait returns the first count batches once they arrived.
func (api *fakeAPI) wait(t *testing.T, count int) []submitted {
	t.Helper()
	for {
		api.mutex.Lock()
		if len(api.batches) >= count {
			defer api.mutex.Unlock()
			return append([]submitted{}, api.batches[:count]...)
		}
		api.mutex.Unlock()
		select {
		case <-api.arrived:
		case <-time.After(10 * time.Second):
			t.Fatalf("%d of %d batches arrived", len(api.batches), count)
		}
	}
}

func (api *fakeAPI) count() int {
	api.mutex.Lock()
	defer api.mutex.Unlock()
	return len(api.batches)
}

// deliveryJSON is a TurnDelivery of the fixture turn, to be submitted to url.
func deliveryJSON(t *testing.T, url string, deadline *time.Time) []byte {
	t.Helper()
	var turn map[string]any
	if err := json.Unmarshal(fixtureTurnJSON, &turn); err != nil {
		t.Fatal(err)
	}
	turn["turn_deadline_at"] = deadline
	data, err := json.Marshal(map[string]any{"turn": turn, "submit": map[string]any{"url": url, "token": "ibst1.test", "deadline_at": deadline}})
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// request sends one request and returns the response with its body read.
func request(t *testing.T, server *httptest.Server, method, path string, body io.Reader, headers map[string]string) (*http.Response, string) {
	t.Helper()
	request, err := http.NewRequest(method, server.URL+path, body)
	if err != nil {
		t.Fatal(err)
	}
	for name, value := range headers {
		if name == "Host" {
			request.Host = value
			continue
		}
		request.Header.Set(name, value)
	}
	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = response.Body.Close() }()
	read, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	return response, string(read)
}

var jsonHeaders = map[string]string{"Content-Type": "application/json"}

func TestServeHealth(t *testing.T) {
	server, _ := startBot(t, sender, time.Second)
	response, body := request(t, server, http.MethodGet, "/health", nil, nil)
	if response.StatusCode != http.StatusOK || body != `{"name":"mybot","ok":true}` || response.Header.Get("Content-Type") != "application/json" ||
		response.Header.Get("Vary") != "Origin" || !response.Close || response.Header.Get("Access-Control-Allow-Origin") != "" {
		t.Fatalf("got %d %q %v", response.StatusCode, body, response.Header)
	}
	response, _ = request(t, server, http.MethodGet, "/health", nil, map[string]string{"Origin": "http://localhost:3000/"})
	if response.Header.Get("Access-Control-Allow-Origin") != "http://localhost:3000/" {
		t.Fatalf("an allowed origin got %v", response.Header)
	}
	localhost := strings.Replace(server.URL, "127.0.0.1", "localhost", 1)
	response, _ = request(t, server, http.MethodGet, "/health", nil, map[string]string{"Host": strings.TrimPrefix(localhost, "http://")})
	if response.StatusCode != http.StatusOK {
		t.Fatalf("localhost as the Host got %d", response.StatusCode)
	}
}

func TestServeAcceptsATurnAndSubmitsItsResult(t *testing.T) {
	api := startAPI(t)
	var played Turn
	var playedMutex sync.Mutex
	server, _ := startBot(t, func(ctx context.Context, turn Turn) (Result, error) {
		playedMutex.Lock()
		played = turn
		playedMutex.Unlock()
		time.Sleep(200 * time.Millisecond)
		return sender(ctx, turn)
	}, time.Second)
	started := time.Now()
	response, body := request(t, server, http.MethodPost, "/turn", bytes.NewReader(deliveryJSON(t, api.url(), nil)),
		map[string]string{"Content-Type": "application/json; charset=utf-8", "Origin": "tauri://localhost"})
	if response.StatusCode != http.StatusAccepted || body != `{"accepted":true}` || response.Header.Get("Access-Control-Allow-Origin") != "tauri://localhost" {
		t.Fatalf("got %d %q %v", response.StatusCode, body, response.Header)
	}
	if elapsed := time.Since(started); elapsed > 150*time.Millisecond {
		t.Fatalf("the answer waited %s for the bot", elapsed)
	}
	got := api.wait(t, 1)[0]
	batch := got.batch
	if got.authorization != "Bearer ibst1.test" || batch.GameID != 1 || batch.GenerationID != 12 || !batch.Complete || len(batch.Commands) != 1 ||
		string(batch.Memory) != `{"turns":1}` || len(batch.Logs) != 1 || batch.Logs[0].Message != "sending" || len(batch.RequestID) != 36 {
		t.Fatalf("submitted %+v", got)
	}
	playedMutex.Lock()
	defer playedMutex.Unlock()
	if played.Generation.ID != 12 || played.TeamColor != 1 {
		t.Fatalf("the bot played %+v", played)
	}
}

// passLine is the one line of a batch that passes the turn.
func passLine(t *testing.T, batch registerCommands) string {
	t.Helper()
	if len(batch.Commands) != 0 || !batch.Complete || len(batch.Logs) != 1 || batch.Logs[0].Level != LogError || batch.Logs[0].Logger != BotsLogger {
		t.Fatalf("not a pass: %+v", batch)
	}
	return batch.Logs[0].Message
}

func TestServePassesTheTurnOfABadBot(t *testing.T) {
	for _, test := range []struct {
		name string
		play Play
		text *regexp.Regexp
	}{
		{"error", func(context.Context, Turn) (Result, error) { return Result{}, errors.New("no plan") }, regexp.MustCompile(`^The bot failed on generation 12, so it passes this turn: no plan$`)},
		{"panic", func(context.Context, Turn) (Result, error) { panic("boom") }, regexp.MustCompile(`^The bot failed on generation 12, so it passes this turn: panic: boom$`)},
		{"invalid result", func(context.Context, Turn) (Result, error) { return Result{Logs: []LogLine{Log("")}}, nil },
			regexp.MustCompile(`so it passes this turn: invalid result: logs\[0\]: the message is empty$`)},
		{"too much memory", func(context.Context, Turn) (Result, error) {
			return Result{Memory: Memory(`{"a":"` + strings.Repeat("x", MemoryLimitBytes) + `"}`)}, nil
		}, regexp.MustCompile(`so it passes this turn: memory is too large: memory is 65544 bytes`)},
		{"slow", func(context.Context, Turn) (Result, error) {
			time.Sleep(time.Second)
			return Result{}, nil
		}, regexp.MustCompile(`^The bot took longer than 100ms on generation 12, so it passes this turn\. It may be in an endless loop\.$`)},
		{"slow and cancelled", func(ctx context.Context, _ Turn) (Result, error) {
			<-ctx.Done()
			return Result{}, ctx.Err()
		}, regexp.MustCompile(`^The bot took longer than 100ms`)},
	} {
		api := startAPI(t)
		server, _ := startBot(t, test.play, 100*time.Millisecond)
		if response, body := request(t, server, http.MethodPost, "/turn", bytes.NewReader(deliveryJSON(t, api.url(), nil)), jsonHeaders); response.StatusCode != http.StatusAccepted {
			t.Fatalf("%s: got %d %q", test.name, response.StatusCode, body)
		}
		batch := api.wait(t, 1)[0].batch
		if message := passLine(t, batch); !test.text.MatchString(message) || batch.Memory != nil {
			t.Errorf("%s: passed with %q, memory %s", test.name, message, batch.Memory)
		}
		// The server keeps serving.
		if response, _ := request(t, server, http.MethodGet, "/health", nil, nil); response.StatusCode != http.StatusOK {
			t.Errorf("%s: then /health got %d", test.name, response.StatusCode)
		}
	}
}

func TestServeKeepsTheMemoryOfAPassedTurn(t *testing.T) {
	api := startAPI(t)
	server, _ := startBot(t, func(context.Context, Turn) (Result, error) { return Result{}, errors.New("no plan") }, time.Second)
	delivery := bytes.Replace(deliveryJSON(t, api.url(), nil), []byte(`"memory":null`), []byte(`"memory":{"kept":true}`), 1)
	if response, _ := request(t, server, http.MethodPost, "/turn", bytes.NewReader(delivery), jsonHeaders); response.StatusCode != http.StatusAccepted {
		t.Fatalf("got %d", response.StatusCode)
	}
	if batch := api.wait(t, 1)[0].batch; string(batch.Memory) != `{"kept":true}` {
		t.Fatalf("memory %s", batch.Memory)
	}
}

func TestServeStopsTheBotBeforeTheDeadline(t *testing.T) {
	api := startAPI(t)
	server, _ := startBot(t, func(ctx context.Context, _ Turn) (Result, error) {
		<-ctx.Done()
		return Result{}, ctx.Err()
	}, time.Minute)
	deadline := time.Now().Add(1500 * time.Millisecond)
	started := time.Now()
	if response, _ := request(t, server, http.MethodPost, "/turn", bytes.NewReader(deliveryJSON(t, api.url(), &deadline)), jsonHeaders); response.StatusCode != http.StatusAccepted {
		t.Fatalf("got %d", response.StatusCode)
	}
	message := passLine(t, api.wait(t, 1)[0].batch)
	if elapsed := time.Since(started); elapsed > 1500*time.Millisecond || !strings.HasPrefix(message, "The bot took longer than ") {
		t.Fatalf("passed after %s with %q", elapsed, message)
	}
}

func TestServePassesARejectedAnswer(t *testing.T) {
	api := startAPI(t, fakeAnswer{409, `{"type":"urn:island-battles:problem:insufficient-soldiers","detail":"castle 0 has 1 soldier left"}`})
	server, _ := startBot(t, sender, time.Second)
	if response, _ := request(t, server, http.MethodPost, "/turn", bytes.NewReader(deliveryJSON(t, api.url(), nil)), jsonHeaders); response.StatusCode != http.StatusAccepted {
		t.Fatalf("got %d", response.StatusCode)
	}
	batches := api.wait(t, 2)
	message := passLine(t, batches[1].batch)
	if len(batches[0].batch.Commands) != 1 || batches[1].batch.RequestID == batches[0].batch.RequestID ||
		message != "The server rejected the bot's answer on generation 12, so it passes this turn: castle 0 has 1 soldier left" {
		t.Fatalf("submitted %+v", batches)
	}
}

func TestServeRetriesAServerErrorButNotAConflict(t *testing.T) {
	api := startAPI(t, fakeAnswer{503, `{}`}, fakeAnswer{409, `{"detail":"player turn is already complete"}`})
	server, bot := startBot(t, sender, time.Second)
	if response, _ := request(t, server, http.MethodPost, "/turn", bytes.NewReader(deliveryJSON(t, api.url(), nil)), jsonHeaders); response.StatusCode != http.StatusAccepted {
		t.Fatalf("got %d", response.StatusCode)
	}
	batches := api.wait(t, 2)
	bot.turns.Wait()
	if batches[0].batch.RequestID != batches[1].batch.RequestID || api.count() != 2 {
		t.Fatalf("submitted %+v", batches)
	}
}

func TestServePlaysATurnDeliveredTwiceOnce(t *testing.T) {
	api := startAPI(t)
	release := make(chan struct{})
	server, bot := startBot(t, func(ctx context.Context, turn Turn) (Result, error) {
		<-release
		return sender(ctx, turn)
	}, 5*time.Second)
	for range 2 {
		if response, _ := request(t, server, http.MethodPost, "/turn", bytes.NewReader(deliveryJSON(t, api.url(), nil)), jsonHeaders); response.StatusCode != http.StatusAccepted {
			t.Fatalf("got %d", response.StatusCode)
		}
	}
	close(release)
	bot.turns.Wait()
	if api.count() != 1 {
		t.Fatalf("%d batches", api.count())
	}
}

func TestServeRefusesASubmitHostThatIsNotAllowed(t *testing.T) {
	api := startAPI(t)
	played := make(chan struct{}, 10)
	server, bot := startBot(t, func(ctx context.Context, turn Turn) (Result, error) {
		played <- struct{}{}
		return sender(ctx, turn)
	}, time.Second)
	for _, url := range []string{
		"https://evil.example.com/generation/commands",
		"http://island-battles.elidan.org/generation/commands",
		"http://192.168.1.2:8888/generation/commands",
		"https://user:secret@island-battles.elidan.org/generation/commands",
		"https://island-battles.elidan.org.evil.com/generation/commands",
		"not a url",
	} {
		response, body := request(t, server, http.MethodPost, "/turn", bytes.NewReader(deliveryJSON(t, url, nil)), jsonHeaders)
		if response.StatusCode != http.StatusBadRequest || !strings.Contains(body, "is not an allowed host") || !strings.Contains(body, "--allow-submit-host") {
			t.Errorf("%s: got %d %q", url, response.StatusCode, body)
		}
	}
	bot.turns.Wait()
	if len(played) != 0 || api.count() != 0 {
		t.Fatalf("the bot played %d turns and sent %d batches", len(played), api.count())
	}
	for _, url := range []string{"https://island-battles.elidan.org/generation/commands", "http://localhost:8888/x", "http://127.0.0.1/x"} {
		if refusal := bot.submitRefusal(url); refusal != "" {
			t.Errorf("%s: %s", url, refusal)
		}
	}
	bot.addSubmitHosts("game.example.org", "Other.example.org:8443")
	for url, allowed := range map[string]bool{
		"https://game.example.org/generation/commands": true,
		"https://game.example.org:444/x":               true,
		"https://other.example.org:8443/x":             true,
		"https://other.example.org/x":                  false,
		"http://game.example.org/x":                    false,
	} {
		if (bot.submitRefusal(url) == "") != allowed {
			t.Errorf("%s: allowed is not %v", url, allowed)
		}
	}
}

func TestServePreflight(t *testing.T) {
	server, _ := startBot(t, sender, time.Second, "https://game.example.org/")
	for _, origin := range []string{"http://localhost:8888", "https://island-battles.elidan.org", "http://tauri.localhost", "https://game.example.org"} {
		response, body := request(t, server, http.MethodOptions, "/turn", nil, map[string]string{"Origin": origin, "Access-Control-Request-Headers": "content-type, x-trace"})
		headers := response.Header
		if response.StatusCode != http.StatusNoContent || body != "" || headers.Get("Access-Control-Allow-Origin") != origin ||
			headers.Get("Access-Control-Allow-Methods") != "GET, POST, OPTIONS" || headers.Get("Access-Control-Allow-Headers") != "content-type, x-trace" ||
			headers.Get("Access-Control-Allow-Private-Network") != "true" || headers.Get("Access-Control-Max-Age") != "600" || headers.Get("Vary") != "Origin" {
			t.Fatalf("%s got %d %v", origin, response.StatusCode, headers)
		}
	}
	response, _ := request(t, server, http.MethodOptions, "/turn", nil, map[string]string{"Origin": "http://localhost:3000"})
	if response.Header.Get("Access-Control-Allow-Headers") != "content-type" {
		t.Fatalf("got %v", response.Header)
	}
	for name, headers := range map[string]map[string]string{
		"no origin":      {},
		"another origin": {"Origin": "https://evil.example.org"},
		"another host":   {"Origin": "http://localhost:3000", "Host": "evil.example.org"},
	} {
		if response, body := request(t, server, http.MethodOptions, "/turn", nil, headers); response.StatusCode != http.StatusForbidden || body != "origin or host not allowed\n" {
			t.Errorf("%s got %d %q", name, response.StatusCode, body)
		}
	}
}

func TestServeRefusals(t *testing.T) {
	api := startAPI(t)
	server, bot := startBot(t, sender, time.Second)
	host := strings.TrimPrefix(server.URL, "http://")
	_, port, _ := net.SplitHostPort(host)
	turn := func() io.Reader { return bytes.NewReader(deliveryJSON(t, api.url(), nil)) }
	for _, test := range []struct {
		name    string
		method  string
		path    string
		body    io.Reader
		headers map[string]string
		status  int
		text    string
	}{
		{"another host", http.MethodGet, "/health", nil, map[string]string{"Host": "evil.example.org:" + port}, 403,
			fmt.Sprintf("Host evil.example.org:%s is not allowed; use 127.0.0.1:%s or localhost:%s\n", port, port, port)},
		{"another port", http.MethodPost, "/turn", turn(), map[string]string{"Host": "127.0.0.1:1", "Content-Type": "application/json"}, 403, ""},
		{"another origin", http.MethodPost, "/turn", turn(), map[string]string{"Origin": "https://evil.example.org", "Content-Type": "application/json"}, 403,
			"origin https://evil.example.org is not allowed; start the server with --allow-origin to add it\n"},
		{"another origin's GET", http.MethodGet, "/health", nil, map[string]string{"Origin": "null"}, 403, ""},
		{"another path", http.MethodGet, "/", nil, nil, 404, "not found\n"},
		{"another POST path", http.MethodPost, "/turns", turn(), jsonHeaders, 404, "not found\n"},
		{"another method", http.MethodPut, "/turn", turn(), jsonHeaders, 405, ""},
		{"not JSON", http.MethodPost, "/turn", turn(), map[string]string{"Content-Type": "text/plain"}, 415, "Content-Type must be application/json\n"},
		{"no type", http.MethodPost, "/turn", turn(), nil, 415, ""},
		{"no length", http.MethodPost, "/turn", io.MultiReader(turn()), jsonHeaders, 411, "Content-Length is required\n"},
		{"an old client's BotTurn", http.MethodPost, "/turn", bytes.NewReader(fixtureTurnJSON), jsonHeaders, 422, "the body is not a TurnDelivery:\nturn is missing\n"},
		{"not a turn", http.MethodPost, "/turn", strings.NewReader(`{"turn": {"game_id": 1}, "submit": {}}`), jsonHeaders, 422, "the body is not a TurnDelivery:\nturn.generation is missing\n"},
		{"no submit URL", http.MethodPost, "/turn", bytes.NewReader(bytes.Replace(deliveryJSON(t, api.url(), nil), []byte(`"url":`), []byte(`"link":`), 1)), jsonHeaders, 422,
			"the body is not a TurnDelivery:\nsubmit.url is missing\n"},
		{"not JSON at all", http.MethodPost, "/turn", strings.NewReader(`{`), jsonHeaders, 422, ""},
	} {
		response, body := request(t, server, test.method, test.path, test.body, test.headers)
		if response.StatusCode != test.status || (test.text != "" && body != test.text) || !response.Close ||
			!strings.HasPrefix(response.Header.Get("Content-Type"), "text/plain") {
			t.Errorf("%s: got %d %q %v", test.name, response.StatusCode, body, response.Header)
		}
	}
	bot.turns.Wait()
	if api.count() != 0 {
		t.Fatalf("a refused request sent %d batches", api.count())
	}
}

func TestServeRefusesATooLargeBody(t *testing.T) {
	server, _ := startBot(t, sender, time.Second)
	connection, err := net.Dial("tcp", strings.TrimPrefix(server.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = connection.Close() }()
	_, _ = fmt.Fprintf(connection, "POST /turn HTTP/1.1\r\nHost: %s\r\nContent-Type: application/json\r\nContent-Length: %d\r\n\r\n{}",
		strings.TrimPrefix(server.URL, "http://"), maxTurnBytes+1)
	response, err := http.ReadResponse(bufio.NewReader(connection), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = response.Body.Close() }()
	body, _ := io.ReadAll(response.Body)
	if response.StatusCode != http.StatusRequestEntityTooLarge || string(body) != "request body is too large\n" {
		t.Fatalf("got %d %q", response.StatusCode, body)
	}
}

// The serve subcommand listens on 127.0.0.1 and stops when its context ends.
func TestServeCommand(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	output, writer := io.Pipe()
	var stderr bytes.Buffer
	stopped := make(chan int, 1)
	go func() {
		stopped <- runMain(ctx, invocation{name: "mybot", play: sender, stdout: writer, stderr: &stderr}, []string{"serve", "--port", "0", "--allow-origin", "https://game.example.org",
			"--allow-submit-host", "game.example.org"})
		_ = writer.Close()
	}()
	lines := bufio.NewScanner(output)
	if !lines.Scan() {
		t.Fatalf("serve printed nothing; stderr %q", stderr.String())
	}
	match := regexp.MustCompile(`^serving mybot on (http://127\.0\.0\.1:\d+) for (.*); submitting to (.*)$`).FindStringSubmatch(lines.Text())
	if match == nil || !strings.Contains(match[2], "https://game.example.org, https://island-battles.elidan.org") ||
		match[3] != "https://game.example.org, https://island-battles.elidan.org and http://localhost" {
		t.Fatalf("serve printed %q", lines.Text())
	}
	// Keep reading the request log, so the server never blocks on it.
	go func() {
		for lines.Scan() {
		}
	}()
	response, err := http.Get(match[1] + "/health")
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("got %d", response.StatusCode)
	}
	cancel()
	select {
	case status := <-stopped:
		if status != 0 {
			t.Fatalf("serve stopped with %d; stderr %q", status, stderr.String())
		}
	case <-time.After(5 * time.Second):
		t.Fatal("serve didn't stop")
	}
}

func TestServeHelpGivesTheLobbysTimeouts(t *testing.T) {
	_, _, stderr := run(t, sender, "serve", "-h")
	text := strings.Join(strings.Fields(stderr), " ")
	if strings.Contains(text, "10s") || strings.Contains(text, "shortest") || !strings.Contains(text, "30s by default") || !strings.Contains(text, "from 5s to 600s") ||
		!strings.Contains(text, "-allow-submit-host") {
		t.Fatalf("help %q", text)
	}
}
