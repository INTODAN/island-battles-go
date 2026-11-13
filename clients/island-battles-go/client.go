package islandbattles

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

// Client plays one player's seat of a lobby's game straight against the
// server, without a browser: the run subcommand uses it.
//
// It reads the game's state over HTTP, hears of new generations on the
// player's Centrifugo channel, and calls the bot once per generation,
// serially. The bot's result is the turn's one completing batch: its
// commands, its game log lines and its memory, which the server stores for
// the next Turn.Memory. When the game ends, the WithGameEnded callback runs
// once and Wait and Run return. While the player waits for others to
// complete a turn that has a deadline, the client asks the server to apply
// the deadline a second after it, which ends the game when someone didn't
// answer.
//
// The bot logs with Logger(ctx). From Start to Close those lines, and the
// SDK's own, go to the console (stdout unless WithLogOutput says otherwise),
// to the WithLogFile file, and, for lines logged while the bot plays, to the
// game log. WithLogLevel applies to all three.
//
// A bot that returns an error or panics, returns an invalid result, or whose
// result the server rejects stops the client, after it sent the error and
// the turn's log lines; the turn stays open. Start the client again for the
// same turn after fixing the bot.
type Client struct {
	urls       URLs
	credential string
	lobbyID    string
	play       Play
	gameEnded  func(ctx context.Context, outcome GameOutcome, generation Generation)
	http       *http.Client
	dial       realtimeDialer
	outputs    *logOutputs
	logFile    string
	sdkLog     *slog.Logger
	timing     clientTiming

	// ctx is cancelled by Close; the background work runs under it.
	ctx    context.Context
	cancel context.CancelFunc

	startMutex sync.Mutex
	// commandMutex orders the batches of commands; reconcileMutex the reads
	// that recover the state.
	commandMutex   sync.Mutex
	reconcileMutex sync.Mutex
	workers        sync.WaitGroup
	closeOnce      sync.Once
	stopOnce       sync.Once

	// The turn worker's own: the upgrade table, read once and reused.
	upgrades []Upgrade

	// tokenMutex guards the player token for the game's routes, and when to
	// get a new one; it is held while one is issued.
	tokenMutex         sync.Mutex
	playerToken        string
	playerTokenRenewAt time.Time

	// mutex guards the fields below.
	mutex      sync.Mutex
	state      *generationState
	identity   *stateIdentity
	connection realtimeConnection
	file       *os.File
	started    bool
	closed     bool
	failure    error
	// lastQueued is the newest generation queued for the bot, and
	// endedQueued whether the state with the outcome was queued: a turn
	// deadline ends the game on a generation already queued.
	lastQueued  uint
	endedQueued bool
	queue       []generationState
	// queued, wakeup and subscribed hold at most one signal; changed is
	// closed and replaced whenever a state is installed; failed and stopped
	// are closed once.
	queued     chan struct{}
	wakeup     chan struct{}
	subscribed chan struct{}
	changed    chan struct{}
	failed     chan struct{}
	stopped    chan struct{}
}

// stateIdentity is what every state of the client's game must keep.
type stateIdentity struct {
	gameID    uint
	playerID  string
	channel   string
	teamColor uint
}

// clientTiming are the client's retry and timeout durations.
type clientTiming struct {
	// retryAttempts is how often a request that may be retried is sent
	// before a transport error is reported.
	retryAttempts int
	// retryDelay is the first wait between attempts, doubling up to
	// retryDelayMax.
	retryDelay    time.Duration
	retryDelayMax time.Duration
	// reconcileDelayMax is the longest wait between failed state reads.
	reconcileDelayMax time.Duration
	// subscriptionTimeout is how long Start waits for the subscription's
	// acknowledgment.
	subscriptionTimeout time.Duration
	// The server runs no timers, so a client waiting for other players asks
	// it to apply the turn's deadline deadlineSlack after the deadline. If
	// the turn is still open after that, for example because the server's
	// clock is behind, it asks again after deadlineRetry, doubling up to
	// deadlineRetryMax.
	deadlineSlack    time.Duration
	deadlineRetry    time.Duration
	deadlineRetryMax time.Duration
	// closeTimeout is how long Close waits for the background work.
	closeTimeout time.Duration
	now          func() time.Time
}

func defaultTiming() clientTiming {
	return clientTiming{
		retryAttempts:       3,
		retryDelay:          100 * time.Millisecond,
		retryDelayMax:       time.Second,
		reconcileDelayMax:   10 * time.Second,
		subscriptionTimeout: 10 * time.Second,
		deadlineSlack:       time.Second,
		deadlineRetry:       2 * time.Second,
		deadlineRetryMax:    30 * time.Second,
		closeTimeout:        2 * time.Second,
		now:                 time.Now,
	}
}

// ClientOption configures a Client.
type ClientOption func(*Client)

// WithHTTPClient sends the client's requests with httpClient instead of an
// http.Client with a 10 s timeout.
func WithHTTPClient(httpClient *http.Client) ClientOption {
	return func(client *Client) { client.http = httpClient }
}

// WithLogLevel sets the lowest level that is logged, to the console, the log
// file and the game log. The default is slog.LevelInfo.
func WithLogLevel(level slog.Level) ClientOption {
	return func(client *Client) { client.outputs.level = level }
}

// WithLogFile also appends the log to a file, which Start creates when it is
// missing.
func WithLogFile(path string) ClientOption {
	return func(client *Client) { client.logFile = path }
}

// WithLogOutput writes the console log to writer instead of os.Stdout, or
// nowhere when writer is nil.
func WithLogOutput(writer io.Writer) ClientOption {
	return func(client *Client) {
		client.outputs.writers = nil
		if writer != nil {
			client.outputs.writers = []io.Writer{writer}
		}
	}
}

// WithGameEnded calls ended once when the game ends, with how it ended and
// the final generation, which has no turn. Wait and Run return after it.
func WithGameEnded(ended func(ctx context.Context, outcome GameOutcome, generation Generation)) ClientOption {
	return func(client *Client) { client.gameEnded = ended }
}

// URLs are where a client reaches Island Battles: Game, the game's site,
// for its routes under /game/ and /generation/, such as
// https://island-battles.example.org, and Lobby, the lobby's, for its routes
// under /lobby/v1/, such as https://lobby.example.org. Where one server
// serves both, as a local one may, BaseURL sets both to its URL.
type URLs struct {
	Game  string
	Lobby string
}

// BaseURL is the shorthand for URLs whose game and lobby are both at
// baseURL, such as http://localhost:8888.
func BaseURL(baseURL string) URLs {
	return URLs{Game: baseURL, Lobby: baseURL}
}

// NewClient makes a client that plays a seat with play: urls are the game's
// and the lobby's URLs, credential the player's guest credential with the
// lobby, and lobbyID the lobby whose game it plays. Start it with Run, or
// with Start and Wait.
func NewClient(urls URLs, credential, lobbyID string, play Play, options ...ClientOption) (*Client, error) {
	canonicalLobbyID, err := canonicalUUID(strings.TrimSpace(lobbyID))
	if err != nil {
		return nil, fmt.Errorf("lobby ID: %w", err)
	}
	if _, err := url.ParseRequestURI(urls.Game); err != nil {
		return nil, fmt.Errorf("game URL: %w", err)
	}
	if _, err := url.ParseRequestURI(urls.Lobby); err != nil {
		return nil, fmt.Errorf("lobby URL: %w", err)
	}
	if play == nil {
		return nil, errors.New("play is nil")
	}
	ctx, cancel := context.WithCancel(context.Background())
	client := &Client{
		urls:       URLs{Game: strings.TrimRight(urls.Game, "/"), Lobby: strings.TrimRight(urls.Lobby, "/")},
		credential: credential,
		lobbyID:    canonicalLobbyID,
		play:       play,
		http:       &http.Client{Timeout: 10 * time.Second},
		dial:       dialCentrifugo,
		outputs:    &logOutputs{level: slog.LevelInfo, writers: []io.Writer{os.Stdout}},
		timing:     defaultTiming(),
		ctx:        ctx,
		cancel:     cancel,
		queued:     make(chan struct{}, 1),
		wakeup:     make(chan struct{}, 1),
		subscribed: make(chan struct{}, 1),
		changed:    make(chan struct{}),
		failed:     make(chan struct{}),
		stopped:    make(chan struct{}),
	}
	for _, option := range options {
		option(client)
	}
	client.sdkLog = slog.New(&logHandler{outputs: client.outputs, name: SDKLogger})
	return client, nil
}

// Generation is the latest board, and false before the first read.
func (client *Client) Generation() (Generation, bool) {
	client.mutex.Lock()
	defer client.mutex.Unlock()
	if client.state == nil {
		return Generation{}, false
	}
	return client.state.Generation, true
}

// TeamColor is the player's team, known after Start, and 0 before.
func (client *Client) TeamColor() uint {
	client.mutex.Lock()
	defer client.mutex.Unlock()
	if client.identity == nil {
		return 0
	}
	return client.identity.teamColor
}

// Outcome is how the game ended, or nil while it runs.
func (client *Client) Outcome() *GameOutcome {
	client.mutex.Lock()
	defer client.mutex.Unlock()
	if client.state == nil {
		return nil
	}
	return client.state.Outcome
}

// Run starts the client, waits until the game ends or the client fails, and
// closes it. It returns nil when the game ended.
func (client *Client) Run(ctx context.Context) error {
	defer func() { _ = client.Close(context.Background()) }()
	if _, err := client.Start(ctx); err != nil {
		return err
	}
	return client.Wait(ctx)
}

// Start connects, subscribes to the player's channel, waits for the
// subscription's acknowledgment and reads the authoritative state, which it
// returns. The bot then plays each new generation in the background. Start
// fails, and closes the client, when the seat isn't played by the player's
// own localhost bot.
func (client *Client) Start(ctx context.Context) (Generation, error) {
	client.startMutex.Lock()
	defer client.startMutex.Unlock()
	if err := client.checkOpen(); err != nil {
		return Generation{}, err
	}
	client.mutex.Lock()
	if client.started {
		generation := client.state.Generation
		client.mutex.Unlock()
		return generation, nil
	}
	client.mutex.Unlock()
	ctx, stop := client.within(ctx)
	defer stop()
	generation, err := client.start(ctx)
	if err != nil {
		err = client.interrupted(ctx, err)
		_ = client.Close(context.Background())
		return Generation{}, err
	}
	return generation, nil
}

func (client *Client) start(ctx context.Context) (Generation, error) {
	if client.logFile != "" {
		file, err := os.OpenFile(client.logFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if err != nil {
			return Generation{}, err
		}
		client.mutex.Lock()
		client.file = file
		client.mutex.Unlock()
		client.outputs.mutex.Lock()
		client.outputs.writers = append(client.outputs.writers, file)
		client.outputs.mutex.Unlock()
	}
	token, err := client.connectionToken(ctx)
	if err != nil {
		return Generation{}, err
	}
	connection, err := client.dial(token.WebsocketURL, token.Token, client.refreshToken, realtimeEvents{
		connected:    client.signalWakeup,
		disconnected: client.signalWakeup,
		subscribed: func() {
			notify(client.subscribed)
			client.signalWakeup()
		},
		subscribing: func() { drain(client.subscribed) },
		unsubscribed: func() {
			drain(client.subscribed)
			client.signalWakeup()
		},
		publication: client.handlePublication,
	})
	if err != nil {
		return Generation{}, newGameError(ErrTransport, 0, fmt.Sprintf("could not connect to %s: %v", token.WebsocketURL, err))
	}
	client.mutex.Lock()
	client.connection = connection
	client.mutex.Unlock()
	// The first authorized read gives the player's channel. Read once to
	// discover it, then again after the subscription's acknowledgment.
	data, err := client.request(ctx, http.MethodGet, client.generationPath(), nil, false)
	if err != nil {
		return Generation{}, err
	}
	initial, err := parseState(data)
	if err != nil {
		return Generation{}, err
	}
	client.mutex.Lock()
	err = client.validateIdentityLocked(initial)
	client.mutex.Unlock()
	if err != nil {
		return Generation{}, err
	}
	if initial.Bot.Kind != "localhost" {
		// The server plays this seat and completes its turns itself.
		return Generation{}, newGameError(ErrRejected, 0, initial.Bot.playedByServer())
	}
	client.mutex.Lock()
	client.identity = &stateIdentity{gameID: initial.GameID, playerID: initial.PlayerID, channel: initial.Channel, teamColor: initial.TeamColor}
	client.mutex.Unlock()
	if err := connection.Subscribe(initial.Channel); err != nil {
		return Generation{}, newGameError(ErrTransport, 0, fmt.Sprintf("could not subscribe to the player channel: %v", err))
	}
	timer := time.NewTimer(client.timing.subscriptionTimeout)
	defer timer.Stop()
	select {
	case <-client.subscribed:
		// Leave the signal for nobody: the reconcile worker reads anyway.
	case <-timer.C:
		// A denied subscription is never acknowledged.
		if _, err := client.fetchState(ctx); err != nil {
			return Generation{}, err
		}
		return Generation{}, newGameError(ErrRejected, 0, "player-channel subscription was not acknowledged")
	case <-ctx.Done():
		return Generation{}, ctx.Err()
	}
	state, err := client.fetchState(ctx)
	if err != nil {
		return Generation{}, err
	}
	client.mutex.Lock()
	client.started = true
	client.mutex.Unlock()
	client.spawn(client.reconcileWorker)
	client.spawn(client.turnWorker)
	client.spawn(client.deadlineWorker)
	return state.Generation, nil
}

// Refresh reads the authoritative state, plays any new generation in the
// background, and returns the board.
func (client *Client) Refresh(ctx context.Context) (Generation, error) {
	if err := client.checkOpen(); err != nil {
		return Generation{}, err
	}
	client.mutex.Lock()
	started := client.started
	client.mutex.Unlock()
	if !started {
		return Generation{}, errors.New("the client has not started")
	}
	ctx, stop := client.within(ctx)
	defer stop()
	client.reconcileMutex.Lock()
	defer client.reconcileMutex.Unlock()
	state, err := client.fetchState(ctx)
	if err != nil {
		return Generation{}, client.interrupted(ctx, err)
	}
	return state.Generation, nil
}

// Wait waits until the game ended, the client was closed or it failed, and
// returns the failure, or nil. It returns ctx's error when ctx is done first.
func (client *Client) Wait(ctx context.Context) error {
	select {
	case <-client.stopped:
	case <-client.failed:
	case <-ctx.Done():
		return ctx.Err()
	}
	client.mutex.Lock()
	defer client.mutex.Unlock()
	return client.failure
}

// Close stops the client without completing an open turn: it cancels the
// bot's ctx and the requests in flight, disconnects and closes the log file.
// It waits up to 2 s, or until ctx is done, for the background work to end.
// Don't call it from the WithGameEnded callback, which Close would wait for.
func (client *Client) Close(ctx context.Context) error {
	client.closeOnce.Do(func() {
		client.mutex.Lock()
		client.closed = true
		connection := client.connection
		client.mutex.Unlock()
		client.stop()
		client.cancel()
		if connection != nil {
			connection.Close()
		}
		done := make(chan struct{})
		go func() {
			client.workers.Wait()
			close(done)
		}()
		timer := time.NewTimer(client.timing.closeTimeout)
		defer timer.Stop()
		select {
		case <-done:
		case <-timer.C:
		case <-ctx.Done():
		}
		client.outputs.mutex.Lock()
		client.mutex.Lock()
		if client.file != nil {
			_ = client.file.Close()
			client.outputs.writers = client.outputs.writers[:len(client.outputs.writers)-1]
			client.file = nil
		}
		client.mutex.Unlock()
		client.outputs.mutex.Unlock()
	})
	return nil
}

// stop makes Wait return.
func (client *Client) stop() {
	client.stopOnce.Do(func() { close(client.stopped) })
}

// within is ctx, also cancelled when the client closes.
func (client *Client) within(ctx context.Context) (context.Context, func()) {
	ctx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(client.ctx, cancel)
	return ctx, func() {
		stop()
		cancel()
	}
}

// interrupted reports an error caused by a cancelled ctx as the client's
// closing, or as ctx's error.
func (client *Client) interrupted(ctx context.Context, err error) error {
	if ctx.Err() == nil {
		return err
	}
	if client.isClosed() {
		return newGameError(ErrClientClosed, 0, "client closed")
	}
	return ctx.Err()
}

func (client *Client) isClosed() bool {
	client.mutex.Lock()
	defer client.mutex.Unlock()
	return client.closed
}

func (client *Client) checkOpen() error {
	client.mutex.Lock()
	defer client.mutex.Unlock()
	switch {
	case client.closed:
		return newGameError(ErrClientClosed, 0, "client closed")
	case client.failure != nil:
		return client.failure
	case client.credential == "":
		return newGameError(ErrAuthentication, 401, "missing credential")
	}
	return nil
}

// fail stops the client with its first failure.
func (client *Client) fail(err error) {
	client.mutex.Lock()
	defer client.mutex.Unlock()
	if client.failure == nil {
		client.failure = err
		close(client.failed)
	}
}

// spawn runs a worker until the client closes. A worker's error fails the
// client, unless the client is closed.
func (client *Client) spawn(worker func() error) {
	client.workers.Add(1)
	go func() {
		defer client.workers.Done()
		if err := worker(); err != nil && !client.isClosed() {
			client.fail(err)
		}
	}()
}

func notify(channel chan struct{}) {
	select {
	case channel <- struct{}{}:
	default:
	}
}

func drain(channel chan struct{}) {
	select {
	case <-channel:
	default:
	}
}

func (client *Client) signalWakeup() { notify(client.wakeup) }

// sleep waits for duration, and returns false when the client closed first.
func (client *Client) sleep(ctx context.Context, duration time.Duration) bool {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-timer.C:
		return true
	case <-ctx.Done():
		return false
	}
}

func (client *Client) generationPath() string {
	return "/game/from-lobby/" + client.lobbyID + "/generation"
}

// gameKey is the game's key in the lobby, the audience of its player tokens.
const gameKey = "island-battles"

// playerTokenRenewShare is the share of a player token's life after which
// the client gets a new one. The lobby's tokens last at most 15 minutes.
const playerTokenRenewShare = 0.8

// request sends one request and returns the body of a successful response:
// with the guest credential to the lobby's routes, and with a player token
// to the game's. The body is encoded once, so a request that fails without
// a response is sent again identically, with the same request ID, when
// retryTransport is set. A game request answered 401 is sent once more with
// a new player token; any other error response is reported, never retried.
// A 401 that remains stops the client.
func (client *Client) request(ctx context.Context, method, path string, payload any, retryTransport bool) ([]byte, error) {
	if err := client.checkOpen(); err != nil {
		return nil, err
	}
	var body []byte
	if payload != nil {
		var err error
		if body, err = json.Marshal(payload); err != nil {
			return nil, fmt.Errorf("could not encode the request: %w", err)
		}
	}
	data, err := client.authorized(ctx, method, path, body, retryTransport)
	if errors.Is(err, ErrAuthentication) {
		client.fail(err)
	}
	return data, err
}

// authorized sends a request with the credential its route takes.
func (client *Client) authorized(ctx context.Context, method, path string, body []byte, retryTransport bool) ([]byte, error) {
	if isLobbyPath(path) {
		return client.attempt(ctx, method, path, body, client.credential, retryTransport)
	}
	token, err := client.gameToken(ctx, "")
	if err != nil {
		return nil, err
	}
	data, err := client.attempt(ctx, method, path, body, token, retryTransport)
	if !errors.Is(err, ErrAuthentication) {
		return data, err
	}
	if token, err = client.gameToken(ctx, token); err != nil {
		return nil, err
	}
	return client.attempt(ctx, method, path, body, token, retryTransport)
}

// gameToken returns the player token for the game's routes. The lobby issues
// a new one once playerTokenRenewShare of the current one's life has passed,
// or when stale, a token the game refused, is still the current one.
func (client *Client) gameToken(ctx context.Context, stale string) (string, error) {
	client.tokenMutex.Lock()
	defer client.tokenMutex.Unlock()
	now := client.timing.now()
	if client.playerToken != "" && client.playerToken != stale && now.Before(client.playerTokenRenewAt) {
		return client.playerToken, nil
	}
	data, err := client.attempt(ctx, http.MethodPost, "/lobby/v1/player-tokens", []byte(`{"game_key":"`+gameKey+`"}`), client.credential, true)
	if err != nil {
		return "", err
	}
	var issued playerToken
	if err := decodeStrict(data, &issued, "player token"); err != nil {
		return "", protocolError("invalid player-token response", err)
	}
	if issued.Token == "" || issued.ExpiresAt.IsZero() {
		return "", protocolError("invalid player-token response", errors.New("no token or expiry"))
	}
	client.playerToken = issued.Token
	client.playerTokenRenewAt = now.Add(time.Duration(float64(issued.ExpiresAt.Sub(now)) * playerTokenRenewShare))
	return issued.Token, nil
}

// attempt sends one request with credential, and again after a transport
// error when retryTransport is set.
func (client *Client) attempt(ctx context.Context, method, path string, body []byte, credential string, retryTransport bool) ([]byte, error) {
	attempts := 1
	if retryTransport {
		attempts = client.timing.retryAttempts
	}
	for attempt := 0; ; attempt++ {
		status, data, err := client.send(ctx, method, path, body, credential)
		if err != nil {
			if ctx.Err() != nil {
				return nil, client.interrupted(ctx, ctx.Err())
			}
			if attempt+1 >= attempts {
				return nil, newGameError(ErrTransport, 0, fmt.Sprintf("network error: %v", err))
			}
			if !client.sleep(ctx, min(client.timing.retryDelay<<attempt, client.timing.retryDelayMax)) {
				return nil, client.interrupted(ctx, ctx.Err())
			}
			continue
		}
		if status >= 400 {
			var answer problem
			_ = json.Unmarshal(data, &answer)
			return nil, responseError(status, answer.Type, answer.Detail)
		}
		if !json.Valid(data) {
			return nil, newGameError(ErrProtocol, 0, "invalid JSON response")
		}
		return data, nil
	}
}

// isLobbyPath tells whether a path is the lobby's, which the client sends to
// the lobby's URL with the guest credential; every other path is the game's.
func isLobbyPath(path string) bool {
	return strings.HasPrefix(path, "/lobby/")
}

// send sends one request with credential, to the lobby's URL or the game's
// by its path, and reads the whole response.
func (client *Client) send(ctx context.Context, method, path string, body []byte, credential string) (int, []byte, error) {
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	base := client.urls.Game
	if isLobbyPath(path) {
		base = client.urls.Lobby
	}
	request, err := http.NewRequestWithContext(ctx, method, base+path, reader)
	if err != nil {
		return 0, nil, err
	}
	request.Header.Set("Authorization", "Bearer "+credential)
	request.Header.Set("Accept", "application/json")
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := client.http.Do(request)
	if err != nil {
		return 0, nil, err
	}
	defer func() { _ = response.Body.Close() }()
	data, err := io.ReadAll(response.Body)
	if err != nil {
		return 0, nil, err
	}
	return response.StatusCode, data, nil
}

func (client *Client) connectionToken(ctx context.Context) (connectionToken, error) {
	data, err := client.request(ctx, http.MethodPost, "/lobby/v1/connection-token", struct{}{}, false)
	if err != nil {
		return connectionToken{}, err
	}
	var token connectionToken
	if err := decodeStrict(data, &token, "connection token"); err != nil {
		return connectionToken{}, protocolError("invalid connection-token response", err)
	}
	return token, nil
}

// refreshToken gets a new connection token for the realtime connection.
func (client *Client) refreshToken() (string, error) {
	token, err := client.connectionToken(client.ctx)
	if err != nil {
		return "", err
	}
	return token.Token, nil
}

func (client *Client) fetchUpgrades(ctx context.Context) ([]Upgrade, error) {
	data, err := client.request(ctx, http.MethodGet, "/game/upgrades", nil, true)
	if err != nil {
		return nil, err
	}
	var table upgradeTable
	if err := decodeStrict(data, &table, "upgrade table"); err != nil {
		return nil, protocolError("invalid upgrade table", err)
	}
	return table.Upgrades, nil
}

// fetchState reads the authoritative state and installs it.
func (client *Client) fetchState(ctx context.Context) (generationState, error) {
	data, err := client.request(ctx, http.MethodGet, client.generationPath(), nil, false)
	if err != nil {
		return generationState{}, err
	}
	state, err := parseState(data)
	if err != nil {
		return generationState{}, err
	}
	client.mutex.Lock()
	defer client.mutex.Unlock()
	return client.installLocked(state)
}

// handlePublication installs a valid event of the client's own game, and
// ignores anything else: HTTP is authoritative. It never waits on HTTP or
// the bot.
func (client *Client) handlePublication(data []byte) {
	state, err := parseEvent(data)
	if err != nil {
		return
	}
	client.mutex.Lock()
	defer client.mutex.Unlock()
	if client.closed {
		return
	}
	_, _ = client.installLocked(state)
}

func (client *Client) validateIdentityLocked(state generationState) error {
	lobbyID, _ := canonicalUUID(state.LobbyID)
	if lobbyID != client.lobbyID || state.Channel != "player:"+state.PlayerID {
		return newGameError(ErrProtocol, 0, "generation state has wrong lobby or player channel")
	}
	if identity := client.identity; identity != nil && (state.GameID != identity.gameID || state.PlayerID != identity.playerID || state.Channel != identity.channel || state.TeamColor != identity.teamColor) {
		return newGameError(ErrProtocol, 0, "generation state has wrong game, player, or team color")
	}
	if current := client.state; current != nil && (state.GameID != current.GameID || state.PlayerID != current.PlayerID) {
		return newGameError(ErrProtocol, 0, "generation state has wrong game or player")
	}
	return nil
}

// installLocked installs a state of the client's game if it is newer, and
// queues a new generation, or the game's end, for the turn worker. A state
// never goes backwards: an older generation is ignored, and within one
// generation neither a completed turn nor the outcome is undone.
func (client *Client) installLocked(state generationState) (generationState, error) {
	if err := client.validateIdentityLocked(state); err != nil {
		return generationState{}, err
	}
	if current := client.state; current != nil {
		if state.GenerationID < current.GenerationID {
			return *current, nil
		}
		if state.GenerationID == current.GenerationID {
			if current.Outcome != nil && state.Outcome == nil {
				// A read begun before a turn deadline ended the game can
				// finish afterward. The outcome is final.
				return *current, nil
			}
			if current.Submitted && !state.Submitted {
				// A read begun before a successful batch can finish
				// afterward. A deadline that ends the game on this
				// generation still brings its outcome.
				if state.Outcome == nil || current.Outcome != nil {
					return *current, nil
				}
				state.Submitted = true
			}
			// A pushed event says 0 stored lines, which a restarted client
			// may receive after its read.
			state.LogLinesStored = max(state.LogLinesStored, current.LogLinesStored)
		}
	}
	client.state = &state
	close(client.changed)
	client.changed = make(chan struct{})
	ends := state.Outcome != nil && !client.endedQueued
	if state.GenerationID > client.lastQueued || ends {
		client.lastQueued = state.GenerationID
		client.endedQueued = state.Outcome != nil
		client.queue = append(client.queue, state)
		notify(client.queued)
	}
	return state, nil
}

// reconcileWorker reads the state after every connect, reconnect or
// resubscribe, the only way to recover a push missed while offline, and
// retries failed reads with backoff. It never polls while idle.
func (client *Client) reconcileWorker() error {
	failures := 0
	for {
		select {
		case <-client.ctx.Done():
			return nil
		case <-client.wakeup:
		}
		client.reconcileMutex.Lock()
		_, err := client.fetchState(client.ctx)
		client.reconcileMutex.Unlock()
		switch {
		case err == nil:
			failures = 0
		case errors.Is(err, ErrTransport):
			failures++
			if !client.sleep(client.ctx, min(client.timing.retryDelay<<min(failures-1, 7), client.timing.reconcileDelayMax)) {
				return nil
			}
			client.signalWakeup()
		default:
			return err
		}
	}
}

// nextQueued waits for the next queued state, and returns false when the
// client closed.
func (client *Client) nextQueued() (generationState, bool) {
	for {
		client.mutex.Lock()
		if len(client.queue) > 0 {
			state := client.queue[0]
			client.queue = client.queue[1:]
			client.mutex.Unlock()
			return state, true
		}
		client.mutex.Unlock()
		select {
		case <-client.ctx.Done():
			return generationState{}, false
		case <-client.queued:
		}
	}
}

// turnWorker plays the queued generations, one at a time, and reports the
// game's end.
func (client *Client) turnWorker() error {
	for {
		state, open := client.nextQueued()
		if !open {
			return nil
		}
		if state.Outcome != nil {
			// The final generation has no turn: report the end, then stop.
			if client.gameEnded != nil {
				client.gameEnded(client.ctx, *state.Outcome, state.Generation)
			}
			client.stop()
			return nil
		}
		client.mutex.Lock()
		latest := *client.state
		client.mutex.Unlock()
		if latest.GenerationID != state.GenerationID || latest.Submitted || latest.Outcome != nil {
			continue
		}
		// Count on from the lines an earlier run stored for this turn, as
		// the latest state of this generation says.
		err := client.playTurn(state, int(latest.LogLinesStored))
		if errors.Is(err, ErrGameEnded) {
			// The turn's deadline passed while the bot played, and the game
			// ended. Reading the state reports the end.
			client.signalWakeup()
			continue
		}
		if err != nil {
			return err
		}
	}
}

// playTurn calls the bot for a generation and sends its result as the
// turn's completing batch. When the bot fails, or its result is rejected,
// it sends the error and the turn's remaining lines in a last batch that
// leaves the turn open, and returns the error.
func (client *Client) playTurn(state generationState, stored int) error {
	log := newGameLog(state.GenerationID, stored)
	turnLog := newTurnLog(log, client.outputs)
	if client.upgrades == nil {
		upgrades, err := client.fetchUpgrades(client.ctx)
		if err != nil {
			return err
		}
		client.upgrades = upgrades
	}
	turn := Turn{
		GameID:         state.GameID,
		Generation:     state.Generation,
		TeamColor:      state.TeamColor,
		Upgrades:       client.upgrades,
		Memory:         state.Memory,
		TurnDeadlineAt: state.TurnDeadlineAt,
	}
	result, err := client.callBot(turnLog, turn)
	turnLog.close()
	if err == nil {
		turnLog.addResult(result)
		if client.isClosed() {
			return newGameError(ErrClientClosed, 0, "client closed")
		}
		_, err = client.register(client.ctx, result.Commands, true, state.GenerationID, true, result.Memory, log)
		if err == nil {
			return nil
		}
	}
	if client.isClosed() || errors.Is(err, ErrGameEnded) || errors.Is(err, ErrTransport) || errors.Is(err, ErrAuthentication) || errors.Is(err, ErrClientClosed) {
		return err
	}
	// The bot failed, returned an invalid result, or the server rejected
	// the result. The turn stays open, and the client stops after the error
	// and the remaining lines are stored.
	turnLog.sdkLog(slog.LevelError, failureMessage(state.GenerationID, err))
	if _, sendError := client.register(client.ctx, nil, false, state.GenerationID, true, nil, log); sendError != nil {
		client.sdkLog.Warn("could not send the failed generation's logs: " + sendError.Error())
	}
	return err
}

// callBot plays one turn in its own goroutine, with ctx cancelled at the
// turn's deadline and when the client closes. It stops waiting when the
// client closes; a bot that ignores ctx keeps its goroutine until it
// returns.
func (client *Client) callBot(turnLog *turnLog, turn Turn) (Result, error) {
	ctx := turnLog.context(client.ctx)
	if turn.TurnDeadlineAt != nil {
		var cancel context.CancelFunc
		ctx, cancel = context.WithDeadline(ctx, *turn.TurnDeadlineAt)
		defer cancel()
	}
	type outcome struct {
		result Result
		err    error
	}
	done := make(chan outcome, 1)
	go func() {
		result, err := callPlay(ctx, client.play, turn)
		done <- outcome{result, err}
	}()
	select {
	case played := <-done:
		return played.result, played.err
	case <-client.ctx.Done():
		return Result{}, newGameError(ErrClientClosed, 0, "client closed")
	}
}

// register sends one batch of commands for a generation, with the game log
// lines logged since the previous batch; last marks the turn's last batch,
// which also carries a held line or the limit error.
func (client *Client) register(ctx context.Context, commands []Command, complete bool, generationID uint, last bool, memory Memory, log *gameLog) (generationState, error) {
	if err := client.checkOpen(); err != nil {
		return generationState{}, err
	}
	client.commandMutex.Lock()
	defer client.commandMutex.Unlock()
	client.mutex.Lock()
	if client.state == nil {
		client.mutex.Unlock()
		return generationState{}, errors.New("the client has not started")
	}
	current := *client.state
	client.mutex.Unlock()
	if complete && (generationID < current.GenerationID || (generationID == current.GenerationID && current.Submitted)) {
		// The turn was already completed; never complete it twice.
		return current, nil
	}
	if commands == nil {
		commands = []Command{}
	}
	var lines []LogLine
	sent := 0
	if log != nil && log.generationID == generationID {
		lines, sent = log.batch(last)
	} else {
		log = nil
	}
	post := func(lines []LogLine) ([]byte, error) {
		batch := registerCommands{
			GameID:       current.GameID,
			GenerationID: generationID,
			RequestID:    newRequestID(),
			Complete:     complete,
			Commands:     commands,
			Memory:       memory,
			Logs:         lines,
		}
		// The request is retried with this exact body, request ID included.
		return client.request(ctx, http.MethodPost, "/generation/commands", batch, true)
	}
	data, err := post(lines)
	switch {
	case errors.Is(err, ErrGameLogLimit) && log != nil && len(lines) > 0:
		// The server holds more lines than this client counted, for example
		// from another client of the same player. Drop the lines, and send
		// the commands and completion without them.
		log.seal()
		var refusal *GameError
		detail := err.Error()
		if errors.As(err, &refusal) {
			detail = refusal.Detail
		}
		client.outputs.write(client.timing.now(), slog.LevelError, SDKLogger, limitError(generationID, -1)+"; the server refused the lines: "+detail)
		data, err = post(nil)
	case errors.Is(err, ErrTransport):
		// The server may have stored the lines; sending them again could
		// pass the line limit, so they are not sent again.
		if log != nil {
			log.commit(sent, last)
		}
		return generationState{}, err
	}
	if err != nil {
		return generationState{}, err
	}
	if log != nil {
		log.commit(sent, last)
	}
	state, err := parseState(data)
	if err != nil {
		return generationState{}, err
	}
	client.mutex.Lock()
	defer client.mutex.Unlock()
	return client.installLocked(state)
}

// deadlineWorker applies the turn's deadline while the player waits for the
// others. The server runs no timers: a turn times out when a request applies
// its deadline. Once this player has completed a turn that has a deadline,
// the client posts to the deadline endpoint a second after it, unless a new
// state arrives first. The server answers with this player's state, which
// carries the outcome when someone didn't answer in time.
func (client *Client) deadlineWorker() error {
	attemptGeneration, attempts := uint(0), 0
	for {
		client.mutex.Lock()
		state, changed := client.state, client.changed
		client.mutex.Unlock()
		if state == nil || state.Outcome != nil || state.TurnDeadlineAt == nil || !state.Submitted {
			select {
			case <-changed:
				continue
			case <-client.ctx.Done():
				return nil
			}
		}
		tries := 0
		if attemptGeneration == state.GenerationID {
			tries = attempts
		}
		delay := state.TurnDeadlineAt.Sub(client.timing.now()) + client.timing.deadlineSlack
		if tries > 0 {
			// The turn was still open after an earlier attempt.
			delay = max(delay, min(client.timing.deadlineRetry<<min(tries-1, 16), client.timing.deadlineRetryMax))
		}
		if delay > 0 {
			timer := time.NewTimer(delay)
			select {
			case <-timer.C:
			case <-changed:
				timer.Stop()
				continue
			case <-client.ctx.Done():
				timer.Stop()
				return nil
			}
		}
		attemptGeneration, attempts = state.GenerationID, tries+1
		client.reconcileMutex.Lock()
		err := client.applyDeadline()
		client.reconcileMutex.Unlock()
		switch {
		case err == nil:
		case errors.Is(err, ErrAuthentication):
			return err
		case client.isClosed():
			return nil
		default:
			// The next attempt waits longer; reads still recover the state.
			client.sdkLog.Warn("could not apply the turn deadline: " + err.Error())
		}
	}
}

func (client *Client) applyDeadline() error {
	data, err := client.request(client.ctx, http.MethodPost, "/game/from-lobby/"+client.lobbyID+"/deadline", nil, true)
	if err != nil {
		return err
	}
	state, err := parseState(data)
	if err != nil {
		return err
	}
	client.mutex.Lock()
	defer client.mutex.Unlock()
	_, err = client.installLocked(state)
	return err
}

// newRequestID is a random UUID (version 4) for a batch.
func newRequestID() string {
	var id [16]byte
	_, _ = rand.Read(id[:])
	id[6] = id[6]&0x0f | 0x40
	id[8] = id[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", id[0:4], id[4:6], id[6:8], id[8:10], id[10:16])
}
