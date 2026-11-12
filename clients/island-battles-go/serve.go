package islandbattles

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"mime"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

// DefaultPort is the Island Battles bot port, where a game client in the
// browser looks for a localhost bot.
const DefaultPort = 41414

// DefaultTurnTimeout is how long serve lets the bot take on a turn before it
// passes the turn. A lobby's turn timeout is 30 s by default and at least
// 5 s; the turn's deadline cuts the bot's time shorter when it comes first.
const DefaultTurnTimeout = 8 * time.Second

// OfficialSubmitHost is the game host that serve submits turns to, over
// https. It also submits to http://localhost and http://127.0.0.1 on any
// port, for a local backend, and to the hosts --allow-submit-host adds.
const OfficialSubmitHost = "island-battles.elidan.org"

// BotsLogger names the game log line that says why a bot passed its turn,
// as the backend names it for its own bots.
const BotsLogger = "island_battles.bots"

// DefaultOrigins are the browser origins that may always relay turns to
// serve: the local backend's lobby page, the web client's dev server, the
// deployed game, and the desktop app's web view (tauri://localhost on macOS
// and Linux, http://tauri.localhost on Windows).
var DefaultOrigins = []string{
	"http://localhost:8888",
	"http://localhost:3000",
	"https://island-battles.elidan.org",
	"tauri://localhost",
	"http://tauri.localhost",
}

const (
	serveHost = "127.0.0.1"
	// maxTurnBytes caps a POST /turn body. A busy 6-team generation with
	// 64 KiB of memory is a few hundred KiB.
	maxTurnBytes = 2 * 1024 * 1024
	// drainBytes is how much of a refused request's body is read, so that
	// closing the connection doesn't reset it before the client reads the
	// refusal.
	drainBytes = 64 * 1024
	// submitMargin is how long before the turn's deadline the bot stops, so
	// that its answer reaches the server in time.
	submitMargin = time.Second
	// submitAttempts is how often an answer is sent when no answer, or a
	// server error, comes back; submitTimeout bounds each attempt.
	submitAttempts = 4
	submitTimeout  = 10 * time.Second
)

// rejectionTypes are the server's command rejections that pass the turn,
// besides every 403 and 422.
var rejectionTypes = map[string]bool{
	"urn:island-battles:problem:upgrade-max-level":     true,
	"urn:island-battles:problem:insufficient-soldiers": true,
}

// originsFlag collects a repeatable flag, such as --allow-origin.
type originsFlag []string

func (origins *originsFlag) String() string { return strings.Join(*origins, ", ") }
func (origins *originsFlag) Set(origin string) error {
	*origins = append(*origins, origin)
	return nil
}

func serve(ctx context.Context, program invocation, arguments []string) int {
	flags := newFlags(program, "serve", fmt.Sprintf("Serves the bot on http://%s:<port> for a game client in the browser: POST /turn\ntakes a TurnDelivery, answers 202 at once, plays the turn and submits the answer\nitself with the delivery's submit token. GET /health says the bot is there.\nGo doesn't reload code: after changing the bot, stop the server and start it again.", serveHost))
	port := flags.Int("port", DefaultPort, "the `port` to serve on")
	turnTimeout := flags.Duration("turn-timeout", DefaultTurnTimeout, "pass the turn when the bot takes longer than this `duration`, or than the time left before the turn's deadline less 1s. A lobby's turn timeout is 30s by default, and its host can set it from 5s to 600s or to none")
	var allowedOrigins, submitHosts originsFlag
	flags.Var(&allowedOrigins, "allow-origin", "also answer this browser `origin`, such as https://game.example.org (repeatable; always allowed: "+strings.Join(DefaultOrigins, ", ")+")")
	flags.Var(&submitHosts, "allow-submit-host", "also submit turns to https://`host`, such as a self-hosted game server (repeatable; always allowed: https://"+OfficialSubmitHost+", and http://localhost and http://127.0.0.1 on any port)")
	if status, ok := parseFlags(flags, arguments); !ok {
		return status
	}
	if *turnTimeout <= 0 {
		_, _ = fmt.Fprintln(program.stderr, "--turn-timeout must be positive")
		return 2
	}
	handler := newBotServer(program.name, program.play, append(slices.Clone(DefaultOrigins), allowedOrigins...), *turnTimeout, program.stdout)
	handler.addSubmitHosts(submitHosts...)
	listener, err := net.Listen("tcp", net.JoinHostPort(serveHost, strconv.Itoa(*port)))
	if err != nil {
		_, _ = fmt.Fprintf(program.stderr, "could not serve: %v\n", err)
		return 1
	}
	server := &http.Server{Handler: handler, ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 10 * time.Second}
	submitTo := []string{}
	for host := range handler.submitHosts {
		submitTo = append(submitTo, "https://"+host)
	}
	slices.Sort(submitTo)
	_, _ = fmt.Fprintf(program.stdout, "serving %s on http://%s for %s; submitting to %s and http://localhost\n", program.name, listener.Addr(),
		strings.Join(slices.Sorted(maps.Keys(handler.origins)), ", "), strings.Join(submitTo, ", "))
	stopped := make(chan struct{})
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = server.Shutdown(shutdown)
		close(stopped)
	}()
	if err := server.Serve(listener); !errors.Is(err, http.ErrServerClosed) {
		_, _ = fmt.Fprintf(program.stderr, "the server stopped: %v\n", err)
		return 1
	}
	<-stopped
	return 0
}

// botServer is the bot's HTTP server: POST /turn, GET /health and the CORS
// preflights for them, as a game client in the browser expects them. It plays
// each delivered turn in the background and submits the answer itself.
type botServer struct {
	name        string
	play        Play
	origins     map[string]bool
	turnTimeout time.Duration
	// submitHosts are the https hosts it submits to, as name or name:port.
	submitHosts map[string]bool
	// httpClient submits the answers.
	httpClient *http.Client
	// logMutex keeps request log lines whole.
	logMutex sync.Mutex
	log      io.Writer
	// botLog is where the bot's Logger writes, besides the result's logs.
	botLog *logOutputs
	// playing are the turns being played, so a turn delivered again isn't
	// played twice.
	playing      map[playingTurn]bool
	playingMutex sync.Mutex
	// turns counts the turns in the background, for tests to wait on.
	turns sync.WaitGroup
}

// playingTurn names a turn being played.
type playingTurn struct {
	url          string
	gameID       uint
	generationID uint
	teamColor    uint
}

func newBotServer(name string, play Play, origins []string, turnTimeout time.Duration, log io.Writer) *botServer {
	allowed := map[string]bool{}
	for _, origin := range origins {
		allowed[strings.TrimRight(origin, "/")] = true
	}
	return &botServer{name: name, play: play, origins: allowed, turnTimeout: turnTimeout, log: log, botLog: &logOutputs{level: slog.LevelInfo, writers: []io.Writer{log}},
		submitHosts: map[string]bool{OfficialSubmitHost: true}, httpClient: &http.Client{Timeout: submitTimeout}, playing: map[playingTurn]bool{}}
}

// addSubmitHosts allows submitting to more https hosts.
func (server *botServer) addSubmitHosts(hosts ...string) {
	for _, host := range hosts {
		server.submitHosts[strings.ToLower(strings.TrimSpace(host))] = true
	}
}

// submitRefusal says why the bot may not submit to submitURL, or is empty
// when it may: https on an allowed host, or http on localhost or 127.0.0.1.
func (server *botServer) submitRefusal(submitURL string) string {
	parsed, err := url.Parse(submitURL)
	if err == nil && parsed.User == nil && parsed.Host != "" {
		host := strings.ToLower(parsed.Hostname())
		switch {
		case parsed.Scheme == "http" && (host == "localhost" || host == "127.0.0.1"):
			return ""
		case parsed.Scheme == "https" && (server.submitHosts[host] || (parsed.Port() != "" && server.submitHosts[host+":"+parsed.Port()])):
			return ""
		}
	}
	return fmt.Sprintf("submit.url %s is not an allowed host, so the bot doesn't play this turn: it submits only to https://%s and to http://localhost or http://127.0.0.1 on any port. Start the server with --allow-submit-host <host> to add an https host.\n", submitURL, OfficialSubmitHost)
}

// logf writes a line to the server's log.
func (server *botServer) logf(format string, arguments ...any) {
	server.logMutex.Lock()
	defer server.logMutex.Unlock()
	_, _ = fmt.Fprintf(server.log, format+"\n", arguments...)
}

// statusWriter remembers the status, for the request log.
type statusWriter struct {
	http.ResponseWriter
	status int
}

func (writer *statusWriter) WriteHeader(status int) {
	writer.status = status
	writer.ResponseWriter.WriteHeader(status)
}

// ServeHTTP answers one request, and closes the connection after it. Never
// keep a connection after a refusal, or the unread body of a cross-origin
// POST could be read as the next request, without an Origin header.
func (server *botServer) ServeHTTP(response http.ResponseWriter, request *http.Request) {
	writer := &statusWriter{ResponseWriter: response, status: http.StatusOK}
	defer func() {
		server.logMutex.Lock()
		defer server.logMutex.Unlock()
		_, _ = fmt.Fprintf(server.log, "%s \"%s %s %s\" %d\n", request.RemoteAddr, request.Method, request.RequestURI, request.Proto, writer.status)
	}()
	switch request.Method {
	case http.MethodOptions:
		server.preflight(writer, request)
	case http.MethodGet:
		if server.refuse(writer, request) {
			return
		}
		if request.URL.Path != "/health" {
			server.text(writer, request, http.StatusNotFound, "not found\n")
			return
		}
		body, _ := json.Marshal(struct {
			Name string `json:"name"`
			OK   bool   `json:"ok"`
		}{server.name, true})
		server.send(writer, request, http.StatusOK, body, "application/json")
	case http.MethodPost:
		if server.refuse(writer, request) {
			return
		}
		if request.URL.Path != "/turn" {
			server.text(writer, request, http.StatusNotFound, "not found\n")
			return
		}
		server.turn(writer, request)
	default:
		server.text(writer, request, http.StatusMethodNotAllowed, "method not allowed\n")
	}
}

// originAllowed is nil without an Origin header (curl), and otherwise
// whether the origin may relay turns.
func (server *botServer) originAllowed(request *http.Request) *bool {
	origin, found := request.Header["Origin"]
	if !found || len(origin) == 0 {
		return nil
	}
	allowed := server.origins[strings.TrimRight(origin[0], "/")]
	return &allowed
}

// hostAllowed accepts only the server's own address as the Host, against DNS
// rebinding.
func (server *botServer) hostAllowed(request *http.Request) bool {
	return request.Host == "127.0.0.1:"+server.port(request) || request.Host == "localhost:"+server.port(request)
}

// port is the port the request came in on.
func (server *botServer) port(request *http.Request) string {
	address, _ := request.Context().Value(http.LocalAddrContextKey).(net.Addr)
	if address == nil {
		return ""
	}
	_, port, _ := net.SplitHostPort(address.String())
	return port
}

// refuse answers 403 to a request for another Host, or from an origin that
// may not relay turns, and reports whether it did.
func (server *botServer) refuse(writer http.ResponseWriter, request *http.Request) bool {
	if !server.hostAllowed(request) {
		port := server.port(request)
		server.text(writer, request, http.StatusForbidden, fmt.Sprintf("Host %s is not allowed; use 127.0.0.1:%s or localhost:%s\n", request.Host, port, port))
		return true
	}
	if allowed := server.originAllowed(request); allowed != nil && !*allowed {
		server.text(writer, request, http.StatusForbidden, fmt.Sprintf("origin %s is not allowed; start the server with --allow-origin to add it\n", request.Header.Get("Origin")))
		return true
	}
	return false
}

// preflight answers a CORS preflight for an allowed origin, with Chrome's
// Private Network Access.
func (server *botServer) preflight(writer http.ResponseWriter, request *http.Request) {
	if allowed := server.originAllowed(request); !server.hostAllowed(request) || allowed == nil || !*allowed {
		server.text(writer, request, http.StatusForbidden, "origin or host not allowed\n")
		return
	}
	headers := writer.Header()
	headers.Set("Access-Control-Allow-Origin", request.Header.Get("Origin"))
	headers.Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
	requested := request.Header.Get("Access-Control-Request-Headers")
	if requested == "" {
		requested = "content-type"
	}
	headers.Set("Access-Control-Allow-Headers", requested)
	headers.Set("Access-Control-Allow-Private-Network", "true")
	headers.Set("Access-Control-Max-Age", "600")
	headers.Set("Vary", "Origin")
	headers.Set("Connection", "close")
	headers.Set("Content-Length", "0")
	writer.WriteHeader(http.StatusNoContent)
}

// turn accepts the TurnDelivery in the body with 202, and plays and submits
// it in the background.
func (server *botServer) turn(writer http.ResponseWriter, request *http.Request) {
	mediaType, _, _ := mime.ParseMediaType(request.Header.Get("Content-Type"))
	if mediaType != "application/json" {
		server.text(writer, request, http.StatusUnsupportedMediaType, "Content-Type must be application/json\n")
		return
	}
	if request.ContentLength < 0 || request.Header.Get("Content-Length") == "" {
		server.text(writer, request, http.StatusLengthRequired, "Content-Length is required\n")
		return
	}
	if request.ContentLength > maxTurnBytes {
		server.text(writer, request, http.StatusRequestEntityTooLarge, "request body is too large\n")
		return
	}
	body, err := io.ReadAll(io.LimitReader(request.Body, maxTurnBytes+1))
	if err != nil {
		server.text(writer, request, http.StatusBadRequest, fmt.Sprintf("could not read the body: %v\n", err))
		return
	}
	delivery, err := DecodeTurnDelivery(body)
	if err != nil {
		server.text(writer, request, http.StatusUnprocessableEntity, fmt.Sprintf("the body is not a TurnDelivery:\n%v\n", err))
		return
	}
	if refusal := server.submitRefusal(delivery.Submit.URL); refusal != "" {
		server.text(writer, request, http.StatusBadRequest, refusal)
		return
	}
	server.send(writer, request, http.StatusAccepted, []byte(`{"accepted":true}`), "application/json")
	key := playingTurn{delivery.Submit.URL, delivery.Turn.GameID, delivery.Turn.Generation.ID, delivery.Turn.TeamColor}
	server.playingMutex.Lock()
	defer server.playingMutex.Unlock()
	if server.playing[key] {
		return
	}
	server.playing[key] = true
	server.turns.Add(1)
	go func() {
		defer server.turns.Done()
		server.submit(delivery, server.playTurn(delivery))
		server.playingMutex.Lock()
		delete(server.playing, key)
		server.playingMutex.Unlock()
	}()
}

// playTurn plays a delivered turn for at most the server's turn timeout, and
// not past the turn's deadline less submitMargin. A bot that returns an error,
// panics, returns an invalid result or runs out of time passes the turn.
func (server *botServer) playTurn(delivery TurnDelivery) Result {
	turn := delivery.Turn
	budget := server.turnTimeout
	if delivery.Submit.DeadlineAt != nil {
		budget = max(0, min(budget, time.Until(*delivery.Submit.DeadlineAt)-submitMargin))
	}
	// The lines the bot logs come first in the result's logs, within the
	// game log's limits.
	turnLog := newTurnLog(newGameLog(turn.Generation.ID, 0), server.botLog)
	ctx, cancel := context.WithTimeout(turnLog.context(context.Background()), budget)
	defer cancel()
	type outcome struct {
		result Result
		err    error
	}
	// A bot stuck in a loop keeps its goroutine, but the server passes the
	// turn and keeps serving.
	done := make(chan outcome, 1)
	go func() {
		result, err := callPlay(ctx, server.play, turn)
		turnLog.close()
		if err == nil {
			turnLog.addResult(result)
			result.Logs, _ = turnLog.gameLog.batch(true)
		}
		done <- outcome{result, err}
	}()
	var played outcome
	select {
	case played = <-done:
	case <-ctx.Done():
	}
	var message string
	switch {
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		message = fmt.Sprintf("The bot took longer than %s on generation %d, so it passes this turn. It may be in an endless loop.", budget.Round(time.Millisecond), turn.Generation.ID)
	case played.err != nil:
		server.logf("%s", failureMessage(turn.Generation.ID, played.err))
		summary, _, _ := strings.Cut(played.err.Error(), "\n")
		message = fmt.Sprintf("The bot failed on generation %d, so it passes this turn: %s", turn.Generation.ID, summary)
	default:
		return played.result
	}
	server.logf("generation %d: %s", turn.Generation.ID, message)
	return passResult(turn, message)
}

// passResult is the answer of a bot that passes its turn: no commands, the
// memory it was given and one error line that says why.
func passResult(turn Turn, message string) Result {
	line := LogAt(LogError, message)
	line.Logger = BotsLogger
	return Result{Memory: turn.Memory, Logs: []LogLine{line}}
}

// submit sends the answer as the turn's completing batch with the delivery's
// token. A server rejection of the answer passes the turn instead.
func (server *botServer) submit(delivery TurnDelivery, result Result) {
	generationID := delivery.Turn.Generation.ID
	status, rejection := server.post(delivery, result)
	if status == http.StatusForbidden || status == http.StatusUnprocessableEntity || (status == http.StatusConflict && rejectionTypes[rejection.Type]) {
		message := fmt.Sprintf("The server rejected the bot's answer on generation %d, so it passes this turn: %s", generationID, rejection.Detail)
		server.logf("generation %d: %s", generationID, message)
		status, rejection = server.post(delivery, passResult(delivery.Turn, message))
	}
	if status >= 200 && status < 300 {
		server.logf("generation %d: submitted %d commands", generationID, len(result.Commands))
		return
	}
	server.logf("generation %d: the server did not take the turn: %d %s", generationID, status, rejection.Detail)
}

// post sends one batch, again after no answer or a server error, and returns
// the last status, 0 for none, and the problem it answered.
func (server *botServer) post(delivery TurnDelivery, result Result) (int, problem) {
	commands := result.Commands
	if commands == nil {
		commands = []Command{}
	}
	body, err := json.Marshal(registerCommands{GameID: delivery.Turn.GameID, GenerationID: delivery.Turn.Generation.ID, RequestID: newRequestID(), Complete: true,
		Commands: commands, Memory: result.Memory, Logs: result.Logs})
	if err != nil {
		return 0, problem{Detail: err.Error()}
	}
	status, answer := 0, problem{}
	for attempt := range submitAttempts {
		if attempt > 0 {
			time.Sleep(min(500*time.Millisecond<<attempt, 4*time.Second))
		}
		request, err := http.NewRequest(http.MethodPost, delivery.Submit.URL, bytes.NewReader(body))
		if err != nil {
			return 0, problem{Detail: err.Error()}
		}
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Authorization", "Bearer "+delivery.Submit.Token)
		response, err := server.httpClient.Do(request)
		if err != nil {
			status, answer = 0, problem{Detail: err.Error()}
			continue
		}
		data, _ := io.ReadAll(io.LimitReader(response.Body, 1<<20))
		_ = response.Body.Close()
		status, answer = response.StatusCode, problem{}
		_ = json.Unmarshal(data, &answer)
		if answer.Detail == "" {
			answer.Detail = http.StatusText(status)
		}
		if status < 500 && status != http.StatusTooManyRequests {
			break
		}
	}
	return status, answer
}

// text answers a plain text body. A refusal first reads a small body, of at
// most drainBytes, so that closing the connection doesn't reset it before the
// client reads the answer. A larger one isn't waited for.
func (server *botServer) text(writer http.ResponseWriter, request *http.Request, status int, text string) {
	if status >= http.StatusBadRequest && request.ContentLength > 0 && request.ContentLength <= drainBytes {
		_, _ = io.Copy(io.Discard, io.LimitReader(request.Body, drainBytes))
	}
	server.send(writer, request, status, []byte(text), "text/plain; charset=utf-8")
}

func (server *botServer) send(writer http.ResponseWriter, request *http.Request, status int, body []byte, contentType string) {
	headers := writer.Header()
	headers.Set("Content-Type", contentType)
	headers.Set("Content-Length", strconv.Itoa(len(body)))
	headers.Set("Connection", "close")
	headers.Set("Vary", "Origin")
	if allowed := server.originAllowed(request); allowed != nil && *allowed {
		headers.Set("Access-Control-Allow-Origin", request.Header.Get("Origin"))
	}
	writer.WriteHeader(status)
	_, _ = writer.Write(body)
}
