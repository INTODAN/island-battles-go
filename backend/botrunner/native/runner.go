// Package native is the native bot runner's code (#223): it plays one turn
// of one uploaded native bot package, such as a Go bot, per event, and
// submits it with the turn's submit token, or delivers a hosted bot's turn
// to the bot's own server. Its event is models.BotRunnerEvent. The Lambda
// function is backend/cmd/bot-runner-native; the local server host runs the
// same Runner in process when no runner functions are configured (#225).
package native

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"islandBattles/backend/hostedbot"
	"islandBattles/backend/models"

	"github.com/google/uuid"
)

const (
	// botsLogger names the game log line that says why a bot passed, as for
	// the default bots (backend/services/go_bots.go).
	botsLogger = "island_battles.bots"
	// deliveryUserAgent is sent with a hosted bot's delivery.
	deliveryUserAgent = "island-battles-bot-delivery/1"
	// submitUserAgent is sent with every submitted batch.
	submitUserAgent = "island-battles-bot-runner/1"
	// deliveryTimeout bounds a hosted bot's delivery.
	deliveryTimeout = 5 * time.Second
	// submitAttemptTimeout bounds each attempt to submit a batch, and
	// submitAttempts is how often a batch is sent when no answer, or a server
	// error, comes back, while the invocation has time left.
	submitAttemptTimeout = 5 * time.Second
	submitAttempts       = 3
)

// rejectionTypes are the server's command rejections that pass the turn,
// besides every 403 and 422, as for the SDKs' serve.
var rejectionTypes = map[string]bool{
	"urn:island-battles:problem:upgrade-max-level":     true,
	"urn:island-battles:problem:insufficient-soldiers": true,
	"urn:island-battles:problem:burst-unavailable":     true,
	"urn:island-battles:problem:burst-above-potential": true,
}

// Outcome is what an invocation did, as the function returns it.
type Outcome struct {
	// Outcome is "submitted" (the bot's answer), "passed" (a pass, because
	// the bot failed), "delivered" (a hosted bot's delivery, or the end of
	// its game, was posted), "unsubmitted" (no submission was taken),
	// "ended" (an uploaded bot took the end of its game) or "unended" (it
	// failed to).
	Outcome string `json:"outcome"`
	// Status is the HTTP status of the last submission or delivery, or 0
	// when none answered.
	Status int `json:"status"`
	// Reason says why the bot passed, or why nothing was taken.
	Reason string `json:"reason,omitempty"`
}

// Runner plays one turn of one bot per invocation. It keeps at most one
// package loaded, with its worker running, between invocations of the same
// instance.
type Runner struct {
	// Directory holds the loaded package, /tmp/bot on Lambda. Loading
	// another package deletes it.
	Directory string
	// SubmitHost is GAME_SUBMIT_HOST: the only host a delivery's submit URL
	// may name.
	SubmitHost string
	// AllowHTTPPackages lets a package's URL use plain http, for local runs
	// and tests. On Lambda the URL is a presigned https GET.
	AllowHTTPPackages bool
	// Client downloads packages and submits batches. Nil uses a client that
	// follows no redirects.
	Client *http.Client
	// AllowLoopbackHosted lets a hosted bot's URL name this machine over
	// http or https (hostedbot.Loopback), for the local server host's runner
	// and tests. On Lambda a hosted bot's URL must pass hostedbot's rules.
	AllowLoopbackHosted bool
	// Logger gets one line for each turn a bot passes.
	Logger *slog.Logger

	loaded *worker
}

// event is the part of a BotRunnerEvent that is passed on as it came: the
// turn the worker plays, a hosted bot's delivery, and the end of the game.
type event struct {
	models.BotRunnerEvent
	rawTurn      json.RawMessage
	rawDelivery  json.RawMessage
	rawGameEnded json.RawMessage
}

func decodeEvent(payload []byte) (event, error) {
	var decoded event
	if err := json.Unmarshal(payload, &decoded.BotRunnerEvent); err != nil {
		return event{}, fmt.Errorf("the event is not a bot runner event: %w", err)
	}
	var raw struct {
		Delivery  json.RawMessage `json:"delivery"`
		GameEnded json.RawMessage `json:"game_ended"`
	}
	if err := json.Unmarshal(payload, &raw); err != nil {
		return event{}, err
	}
	decoded.rawDelivery, decoded.rawGameEnded = raw.Delivery, raw.GameEnded
	if decoded.Delivery == nil {
		return decoded, nil
	}
	var rawDelivery struct {
		Turn json.RawMessage `json:"turn"`
	}
	if err := json.Unmarshal(raw.Delivery, &rawDelivery); err != nil {
		return event{}, fmt.Errorf("the event's delivery is not a turn delivery: %w", err)
	}
	decoded.rawTurn = rawDelivery.Turn
	return decoded, nil
}

// Handle plays the turn of one event: a package's turn, or a hosted bot's
// delivery, or offers a bot the end of its game. An event that can't be
// played at all, such as one whose submit URL isn't on the game's host, is
// an error and submits nothing.
func (runner *Runner) Handle(ctx context.Context, payload json.RawMessage) (Outcome, error) {
	received, err := decodeEvent(payload)
	if err != nil {
		return Outcome{}, err
	}
	switch {
	case received.Delivery != nil && received.GameEnded != nil:
		return Outcome{}, errors.New("the event has both a turn and the end of a game")
	case received.GameEnded != nil:
		return runner.endGame(ctx, received)
	case received.Delivery == nil:
		return Outcome{}, errors.New("the event has neither a turn nor the end of a game")
	}
	delivery := *received.Delivery
	if err := checkSubmitURL(delivery.Submit.URL, runner.SubmitHost); err != nil {
		return Outcome{}, err
	}
	if len(received.rawTurn) == 0 || delivery.Turn.GameID == 0 || delivery.Turn.Generation.ID == 0 {
		return Outcome{}, errors.New("the event's delivery has no turn")
	}
	switch {
	case received.Hosted != nil && received.Package != nil:
		return Outcome{}, errors.New("the event has both a package and a hosted bot")
	case received.Hosted != nil:
		return runner.deliver(ctx, received.Hosted.URL, received.rawDelivery)
	case received.Package == nil:
		return Outcome{}, errors.New("the event has neither a package nor a hosted bot")
	case received.Limits == nil || received.Limits.LoadSeconds <= 0 || received.Limits.PlaySeconds <= 0:
		return Outcome{}, errors.New("a package's event needs positive limits.load_seconds and limits.play_seconds")
	}
	answer, failure := runner.play(ctx, *received.Package, *received.Limits, received.rawTurn)
	if failure != "" {
		return runner.pass(ctx, delivery, failure), nil
	}
	status, rejection := runner.submit(ctx, delivery, answer)
	if rejected(status, rejection) {
		return runner.pass(ctx, delivery, "The server rejected the uploaded bot's answer, so it passes this turn: "+rejection.Detail), nil
	}
	return submitted("submitted", status, rejection), nil
}

// endGame offers a bot the end of its game (#413): it POSTs a hosted bot's
// GameEndedDelivery to the game-ended sibling of its URL, and offers an
// uploaded bot's worker the {"game_ended": ...} line, loading the package
// first if needed. It submits nothing, and a bot that fails changes nothing:
// the outcome only says what happened. The demo link must be on the game's
// host, like a submit URL.
func (runner *Runner) endGame(ctx context.Context, received event) (Outcome, error) {
	ended := received.GameEnded
	if err := checkSubmitURL(ended.Demo.URL, runner.SubmitHost); err != nil {
		return Outcome{}, fmt.Errorf("the demo link: %w", err)
	}
	if ended.GameID == 0 {
		return Outcome{}, errors.New("the event's game_ended has no game")
	}
	switch {
	case received.Hosted != nil && received.Package != nil:
		return Outcome{}, errors.New("the event has both a package and a hosted bot")
	case received.Hosted != nil:
		target, err := hostedbot.GameEndedURL(received.Hosted.URL)
		if err != nil {
			return Outcome{}, fmt.Errorf("the hosted bot's URL is refused: %w", err)
		}
		return runner.deliver(ctx, target, received.rawGameEnded)
	case received.Package == nil:
		return Outcome{}, errors.New("the event has neither a package nor a hosted bot")
	case received.Limits == nil || received.Limits.LoadSeconds <= 0 || received.Limits.PlaySeconds <= 0:
		return Outcome{}, errors.New("a package's event needs positive limits.load_seconds and limits.play_seconds")
	}
	if failure := runner.ensureLoaded(ctx, *received.Package, *received.Limits); failure != "" {
		return runner.endFailed(ctx, ended, failure), nil
	}
	err := runner.loaded.endGame(ctx, received.rawGameEnded, seconds(received.Limits.PlaySeconds))
	var botError *botAnswerError
	switch {
	case err == nil:
		return Outcome{Outcome: "ended"}, nil
	case errors.As(err, &botError):
		return runner.endFailed(ctx, ended, "The uploaded bot failed at the end of its game: "+botError.message), nil
	default:
		stderr := runner.loaded.stderr.String()
		runner.Unload()
		return runner.endFailed(ctx, ended, withOutput("The uploaded bot "+err.Error()+" at the end of its game.", stderr)), nil
	}
}

// endFailed is the outcome of an uploaded bot that failed at the end of its
// game. It is only logged: the game has ended, so there is no game log line
// to add it to.
func (runner *Runner) endFailed(ctx context.Context, ended *models.GameEndedDelivery, reason string) Outcome {
	reason = truncateLogMessage(reason)
	runner.logger().WarnContext(ctx, "An uploaded bot failed at the end of its game", "game_id", ended.GameID, "reason", reason)
	return Outcome{Outcome: "unended", Reason: reason}
}

// ensureLoaded loads the package with its worker, unless the instance has
// it loaded already, within limits.LoadSeconds. It returns why it couldn't.
func (runner *Runner) ensureLoaded(ctx context.Context, botPackage models.BotRunnerPackage, limits models.BotRunnerLimits) string {
	if runner.loaded != nil && (runner.loaded.packageID != botPackage.ID || runner.loaded.stopped()) {
		runner.Unload()
	}
	if runner.loaded != nil {
		return ""
	}
	loadContext, cancel := context.WithTimeout(ctx, seconds(limits.LoadSeconds))
	loaded, err := runner.load(loadContext, botPackage)
	cancel()
	if err != nil {
		runner.Unload()
		var started *loadError
		stderr := ""
		if errors.As(err, &started) {
			stderr = started.stderr
		}
		return withOutput("The uploaded bot could not be loaded: "+err.Error(), stderr)
	}
	runner.loaded = loaded
	return ""
}

// play plays the turn with the package's worker, loading the package first
// if the instance has another one, or none. It returns the worker's result,
// or why the bot passes.
func (runner *Runner) play(ctx context.Context, botPackage models.BotRunnerPackage, limits models.BotRunnerLimits, turn json.RawMessage) (json.RawMessage, string) {
	if failure := runner.ensureLoaded(ctx, botPackage, limits); failure != "" {
		return nil, strings.Replace(failure, "could not be loaded:", "could not be loaded, so it passes this turn:", 1)
	}
	answer, err := runner.loaded.play(ctx, turn, seconds(limits.PlaySeconds))
	var botError *botAnswerError
	switch {
	case err == nil:
		return answer, ""
	case errors.As(err, &botError):
		// The worker is fine; only the bot's turn failed.
		return nil, "The uploaded bot failed this turn, so it passes: " + botError.message
	default:
		// A worker that timed out, crashed or broke the protocol is killed,
		// and the next turn loads the package again.
		stderr := runner.loaded.stderr.String()
		runner.Unload()
		return nil, withOutput("The uploaded bot "+err.Error()+", so it passes this turn and is restarted next turn.", stderr)
	}
}

// Unload stops the worker, if any, and deletes the loaded package.
func (runner *Runner) Unload() {
	if runner.loaded != nil {
		runner.loaded.kill()
		runner.loaded = nil
	}
	_ = os.RemoveAll(runner.Directory)
}

func seconds(value float64) time.Duration {
	return time.Duration(value * float64(time.Second))
}

// withOutput adds the end of the worker's stderr to message, as much as
// fits in a game log line.
func withOutput(message, stderr string) string {
	stderr = strings.TrimSpace(stderr)
	if stderr == "" {
		return truncateLogMessage(message)
	}
	if !strings.HasSuffix(message, ".") {
		message += "."
	}
	const separator = " Its last output: "
	room := models.MaxGenerationLogMessageLength - len([]rune(message)) - len([]rune(separator))
	if room < 20 {
		return truncateLogMessage(message)
	}
	output := []rune(stderr)
	if len(output) > room {
		output = append([]rune("…"), output[len(output)-room+1:]...)
	}
	return message + separator + string(output)
}

// truncateLogMessage cuts a message to a game log line's 500 characters,
// ending it with an ellipsis, as backend/services/go_bots.go does.
func truncateLogMessage(message string) string {
	runes := []rune(message)
	if len(runes) <= models.MaxGenerationLogMessageLength {
		return message
	}
	return string(runes[:models.MaxGenerationLogMessageLength-1]) + "…"
}

// checkSubmitURL refuses a submit URL that isn't on submitHost, so that an
// event can't make the runner post a token, or anything else, elsewhere: it
// must be https on that host, or http when the host is localhost or
// 127.0.0.1, for a local backend.
func checkSubmitURL(submitURL, submitHost string) error {
	if submitHost == "" {
		return errors.New("GAME_SUBMIT_HOST is not set, so the runner submits nowhere")
	}
	parsed, err := url.Parse(submitURL)
	if err != nil || parsed.User != nil || parsed.Host == "" || !strings.EqualFold(parsed.Host, submitHost) {
		return fmt.Errorf("the submit URL %q is not on the game's host %s", submitURL, submitHost)
	}
	hostname := strings.ToLower(parsed.Hostname())
	if parsed.Scheme == "https" || (parsed.Scheme == "http" && (hostname == "localhost" || hostname == "127.0.0.1")) {
		return nil
	}
	return fmt.Errorf("the submit URL %q must use https", submitURL)
}

func (runner *Runner) client() *http.Client {
	if runner.Client != nil {
		return runner.Client
	}
	return &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}

func (runner *Runner) logger() *slog.Logger {
	if runner.Logger != nil {
		return runner.Logger
	}
	return slog.Default()
}

// deliver posts a hosted bot's delivery, or the end of its game, to its URL, and does nothing more:
// the bot submits its answer itself. The URL must pass hostedbot's rules
// again, and the post goes through hostedbot.Client, which connects only to
// public addresses on port 443, checking the address it really connects to,
// so a name that resolves elsewhere since the bot was chosen reaches nothing
// on a private network. It follows no redirects.
func (runner *Runner) deliver(ctx context.Context, hostedURL string, delivery json.RawMessage) (Outcome, error) {
	client := hostedbot.Client(deliveryTimeout)
	if runner.AllowLoopbackHosted && hostedbot.Loopback(hostedURL) {
		client = &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	} else if _, err := hostedbot.CheckSyntax(hostedURL); err != nil {
		return Outcome{}, fmt.Errorf("the hosted bot's URL is refused: %w", err)
	}
	ctx, cancel := context.WithTimeout(ctx, deliveryTimeout)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, hostedURL, bytes.NewReader(delivery))
	if err != nil {
		return Outcome{}, err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("User-Agent", deliveryUserAgent)
	response, err := client.Do(request)
	if err != nil {
		runner.logger().WarnContext(ctx, "The hosted bot's delivery failed", "error", err)
		return Outcome{Outcome: "undelivered", Reason: err.Error()}, nil
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 64<<10))
	_ = response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode > 299 {
		runner.logger().WarnContext(ctx, "The hosted bot refused its delivery", "status", response.StatusCode)
	}
	return Outcome{Outcome: "delivered", Status: response.StatusCode}, nil
}

// problem is the part of an answer's problem details that matters here.
type problem struct {
	Type   string `json:"type"`
	Detail string `json:"detail"`
}

func rejected(status int, rejection problem) bool {
	return status == http.StatusForbidden || status == http.StatusUnprocessableEntity || (status == http.StatusConflict && rejectionTypes[rejection.Type])
}

func submitted(outcome string, status int, answer problem) Outcome {
	if status < 200 || status > 299 {
		return Outcome{Outcome: "unsubmitted", Status: status, Reason: answer.Detail}
	}
	return Outcome{Outcome: outcome, Status: status}
}

// pass submits a pass: no commands, the turn's own memory, and one error
// line that says why, as the default bots pass.
func (runner *Runner) pass(ctx context.Context, delivery models.TurnDelivery, reason string) Outcome {
	reason = truncateLogMessage(reason)
	runner.logger().WarnContext(ctx, "An uploaded bot passes its turn", "game_id", delivery.Turn.GameID, "generation_id", delivery.Turn.Generation.ID, "reason", reason)
	logs, _ := json.Marshal([]models.GenerationLogLine{{Level: models.GenerationLogError, Logger: botsLogger, Message: reason}})
	memory := json.RawMessage("null")
	if len(delivery.Turn.Memory) > 0 {
		memory = json.RawMessage(delivery.Turn.Memory)
	}
	answer, _ := json.Marshal(struct {
		Commands []any           `json:"commands"`
		Logs     json.RawMessage `json:"logs"`
		Memory   json.RawMessage `json:"memory"`
	}{Commands: []any{}, Logs: logs, Memory: memory})
	status, rejection := runner.submit(ctx, delivery, answer)
	outcome := submitted("passed", status, rejection)
	if outcome.Outcome == "passed" {
		outcome.Reason = reason
	}
	return outcome
}

// batch is the completing batch of POST /generation/commands, with the
// worker's result passed on as it came.
type batch struct {
	GameID       uint            `json:"game_id"`
	GenerationID uint            `json:"generation_id"`
	RequestID    string          `json:"request_id"`
	Complete     bool            `json:"complete"`
	Commands     json.RawMessage `json:"commands"`
	Logs         json.RawMessage `json:"logs"`
	Memory       json.RawMessage `json:"memory"`
}

// submit sends a BotTurnResult as the turn's completing batch with the
// delivery's token, again after no answer or a server error while there is
// time, and returns the last status, 0 for none, and its problem.
func (runner *Runner) submit(ctx context.Context, delivery models.TurnDelivery, answer json.RawMessage) (int, problem) {
	var result struct {
		Commands json.RawMessage `json:"commands"`
		Logs     json.RawMessage `json:"logs"`
		Memory   json.RawMessage `json:"memory"`
	}
	if err := json.Unmarshal(answer, &result); err != nil {
		return 0, problem{Detail: "the answer is not a BotTurnResult: " + err.Error()}
	}
	body, err := json.Marshal(batch{GameID: delivery.Turn.GameID, GenerationID: delivery.Turn.Generation.ID, RequestID: uuid.NewString(), Complete: true,
		Commands: orEmpty(result.Commands, "[]"), Logs: orEmpty(result.Logs, "[]"), Memory: orEmpty(result.Memory, "null")})
	if err != nil {
		return 0, problem{Detail: err.Error()}
	}
	status, answered := 0, problem{}
	for attempt := range submitAttempts {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return status, answered
			case <-time.After(250 * time.Millisecond << attempt):
			}
		}
		status, answered = runner.post(ctx, delivery.Submit, body)
		if status != 0 && status < 500 {
			break
		}
	}
	return status, answered
}

func orEmpty(value json.RawMessage, empty string) json.RawMessage {
	if len(bytes.TrimSpace(value)) == 0 || (empty == "[]" && bytes.Equal(bytes.TrimSpace(value), []byte("null"))) {
		return json.RawMessage(empty)
	}
	return value
}

func (runner *Runner) post(ctx context.Context, submit models.TurnSubmit, body []byte) (int, problem) {
	ctx, cancel := context.WithTimeout(ctx, submitAttemptTimeout)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, submit.URL, bytes.NewReader(body))
	if err != nil {
		return 0, problem{Detail: err.Error()}
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("User-Agent", submitUserAgent)
	request.Header.Set("Authorization", "Bearer "+submit.Token)
	response, err := runner.client().Do(request)
	if err != nil {
		return 0, problem{Detail: err.Error()}
	}
	defer func() { _ = response.Body.Close() }()
	data, _ := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	answer := problem{}
	_ = json.Unmarshal(data, &answer)
	return response.StatusCode, answer
}
