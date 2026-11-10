package islandbattles

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"mime"
	"net"
	"net/http"
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
// answers 504: under the lobby's shortest turn timeout of 10 s.
const DefaultTurnTimeout = 8 * time.Second

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
)

// originsFlag collects the repeatable --allow-origin flag.
type originsFlag []string

func (origins *originsFlag) String() string { return strings.Join(*origins, ", ") }
func (origins *originsFlag) Set(origin string) error {
	*origins = append(*origins, origin)
	return nil
}

func serve(ctx context.Context, program invocation, arguments []string) int {
	flags := newFlags(program, "serve", fmt.Sprintf("Serves the bot on http://%s:<port> for a game client in the browser: POST /turn\nplays a BotTurn and answers its BotTurnResult, GET /health says the bot is there.\nGo doesn't reload code: after changing the bot, stop the server and start it again.", serveHost))
	port := flags.Int("port", DefaultPort, "the `port` to serve on")
	turnTimeout := flags.Duration("turn-timeout", DefaultTurnTimeout, "answer 504 when a turn takes longer than this `duration`, under the lobby's shortest turn timeout of 10s")
	var allowedOrigins originsFlag
	flags.Var(&allowedOrigins, "allow-origin", "also answer this browser `origin`, such as https://game.example.org (repeatable; always allowed: "+strings.Join(DefaultOrigins, ", ")+")")
	if status, ok := parseFlags(flags, arguments); !ok {
		return status
	}
	if *turnTimeout <= 0 {
		_, _ = fmt.Fprintln(program.stderr, "--turn-timeout must be positive")
		return 2
	}
	handler := newBotServer(program.name, program.play, append(slices.Clone(DefaultOrigins), allowedOrigins...), *turnTimeout, program.stdout)
	listener, err := net.Listen("tcp", net.JoinHostPort(serveHost, strconv.Itoa(*port)))
	if err != nil {
		_, _ = fmt.Fprintf(program.stderr, "could not serve: %v\n", err)
		return 1
	}
	server := &http.Server{Handler: handler, ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 10 * time.Second}
	_, _ = fmt.Fprintf(program.stdout, "serving %s on http://%s for %s\n", program.name, listener.Addr(), strings.Join(slices.Sorted(maps.Keys(handler.origins)), ", "))
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
// preflights for them, as a game client in the browser expects them.
type botServer struct {
	name        string
	play        Play
	origins     map[string]bool
	turnTimeout time.Duration
	// logMutex keeps request log lines whole.
	logMutex sync.Mutex
	log      io.Writer
}

func newBotServer(name string, play Play, origins []string, turnTimeout time.Duration, log io.Writer) *botServer {
	allowed := map[string]bool{}
	for _, origin := range origins {
		allowed[strings.TrimRight(origin, "/")] = true
	}
	return &botServer{name: name, play: play, origins: allowed, turnTimeout: turnTimeout, log: log}
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

// turn plays the BotTurn in the body and answers its BotTurnResult.
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
	turn, err := DecodeTurn(body)
	if err != nil {
		server.text(writer, request, http.StatusUnprocessableEntity, fmt.Sprintf("the body is not a BotTurn:\n%v\n", err))
		return
	}
	ctx, cancel := context.WithTimeout(request.Context(), server.turnTimeout)
	defer cancel()
	type outcome struct {
		result Result
		err    error
	}
	// A bot stuck in a loop keeps its goroutine, but the server answers and
	// keeps serving.
	done := make(chan outcome, 1)
	go func() {
		result, err := callPlay(ctx, server.play, turn)
		done <- outcome{result, err}
	}()
	var played outcome
	select {
	case played = <-done:
	case <-ctx.Done():
	}
	if request.Context().Err() != nil {
		// The game client went away; nobody reads an answer.
		return
	}
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		server.text(writer, request, http.StatusGatewayTimeout, fmt.Sprintf("the bot took longer than %s on this turn; it may be in an endless loop. Fix it and start the server again; the server keeps serving.\n", server.turnTimeout))
		return
	}
	if played.err != nil {
		server.text(writer, request, http.StatusInternalServerError, played.err.Error()+"\n")
		return
	}
	answer, err := json.Marshal(played.result)
	if err != nil {
		server.text(writer, request, http.StatusInternalServerError, fmt.Sprintf("could not encode the result: %v\n", err))
		return
	}
	server.send(writer, request, http.StatusOK, answer, "application/json")
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
