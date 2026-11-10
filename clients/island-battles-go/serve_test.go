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
	"testing"
	"time"
)

// startBot serves play as serve does, with extra allowed origins.
func startBot(t *testing.T, play Play, turnTimeout time.Duration, extraOrigins ...string) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(newBotServer("mybot", play, append(append([]string{}, DefaultOrigins...), extraOrigins...), turnTimeout, io.Discard))
	t.Cleanup(server.Close)
	return server
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
	server := startBot(t, sender, time.Second)
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

func TestServePlaysATurn(t *testing.T) {
	var played Turn
	server := startBot(t, func(ctx context.Context, turn Turn) (Result, error) {
		played = turn
		return sender(ctx, turn)
	}, time.Second)
	response, body := request(t, server, http.MethodPost, "/turn", bytes.NewReader(fixtureTurnJSON),
		map[string]string{"Content-Type": "application/json; charset=utf-8", "Origin": "tauri://localhost"})
	want := `{"commands":[{"command_type":"send_soldier_group","parameters":{"origin_castle_id":0,"destination_castle_id":1,"soldier_amount":1}}],"memory":{"turns":1},"logs":[{"level":"info","logger":"island_battles.player","message":"sending"}]}`
	if response.StatusCode != http.StatusOK || body != want || response.Header.Get("Access-Control-Allow-Origin") != "tauri://localhost" {
		t.Fatalf("got %d %q %v", response.StatusCode, body, response.Header)
	}
	if played.Generation.ID != 12 || played.TeamColor != 1 {
		t.Fatalf("the bot played %+v", played)
	}
	var result Result
	if err := json.Unmarshal([]byte(body), &result); err != nil || ValidateResult(result) != nil {
		t.Fatalf("the answer is not a result: %v", err)
	}
}

func TestServePreflight(t *testing.T) {
	server := startBot(t, sender, time.Second, "https://game.example.org/")
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
	failing := func(context.Context, Turn) (Result, error) { return Result{}, errors.New("no plan") }
	server := startBot(t, failing, time.Second)
	host := strings.TrimPrefix(server.URL, "http://")
	_, port, _ := net.SplitHostPort(host)
	turn := func() io.Reader { return bytes.NewReader(fixtureTurnJSON) }
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
		{"not a turn", http.MethodPost, "/turn", strings.NewReader(`{"game_id": 1}`), jsonHeaders, 422, "the body is not a BotTurn:\nturn.generation is missing\n"},
		{"not JSON at all", http.MethodPost, "/turn", strings.NewReader(`{`), jsonHeaders, 422, ""},
		{"a failing bot", http.MethodPost, "/turn", turn(), jsonHeaders, 500, "no plan\n"},
	} {
		response, body := request(t, server, test.method, test.path, test.body, test.headers)
		if response.StatusCode != test.status || (test.text != "" && body != test.text) || !response.Close ||
			!strings.HasPrefix(response.Header.Get("Content-Type"), "text/plain") {
			t.Errorf("%s: got %d %q %v", test.name, response.StatusCode, body, response.Header)
		}
	}
}

func TestServeRefusesATooLargeBody(t *testing.T) {
	server := startBot(t, sender, time.Second)
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

func TestServeReportsBadBots(t *testing.T) {
	for _, test := range []struct {
		name   string
		play   Play
		status int
		text   *regexp.Regexp
	}{
		{"panic", func(context.Context, Turn) (Result, error) { panic("boom") }, 500, regexp.MustCompile(`^panic: boom\n\ngoroutine \d+`)},
		{"invalid result", func(context.Context, Turn) (Result, error) { return Result{Logs: []LogLine{Log("")}}, nil }, 500,
			regexp.MustCompile(`^invalid result: logs\[0\]: the message is empty\n$`)},
		{"too much memory", func(context.Context, Turn) (Result, error) {
			return Result{Memory: Memory(`{"a":"` + strings.Repeat("x", MemoryLimitBytes) + `"}`)}, nil
		}, 500, regexp.MustCompile(`^memory is too large: memory is 65544 bytes`)},
		{"slow", func(context.Context, Turn) (Result, error) {
			time.Sleep(time.Second)
			return Result{}, nil
		}, 504, regexp.MustCompile(`^the bot took longer than 100ms on this turn; it may be in an endless loop\. Fix it and start the server again; the server keeps serving\.\n$`)},
		{"slow and cancelled", func(ctx context.Context, _ Turn) (Result, error) {
			<-ctx.Done()
			return Result{}, ctx.Err()
		}, 504, regexp.MustCompile(`^the bot took longer than 100ms`)},
	} {
		server := startBot(t, test.play, 100*time.Millisecond)
		response, body := request(t, server, http.MethodPost, "/turn", bytes.NewReader(fixtureTurnJSON), jsonHeaders)
		if response.StatusCode != test.status || !test.text.MatchString(body) {
			t.Errorf("%s: got %d %q", test.name, response.StatusCode, body)
		}
		// The server keeps serving.
		if response, _ := request(t, server, http.MethodGet, "/health", nil, nil); response.StatusCode != http.StatusOK {
			t.Errorf("%s: then /health got %d", test.name, response.StatusCode)
		}
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
		stopped <- runMain(ctx, invocation{name: "mybot", play: sender, stdout: writer, stderr: &stderr}, []string{"serve", "--port", "0", "--allow-origin", "https://game.example.org"})
		_ = writer.Close()
	}()
	lines := bufio.NewScanner(output)
	if !lines.Scan() {
		t.Fatalf("serve printed nothing; stderr %q", stderr.String())
	}
	match := regexp.MustCompile(`^serving mybot on (http://127\.0\.0\.1:\d+) for (.*)$`).FindStringSubmatch(lines.Text())
	if match == nil || !strings.Contains(match[2], "https://game.example.org, https://island-battles.elidan.org") {
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
