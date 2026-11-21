package islandbattles

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"runtime/debug"
	"strings"
	"time"
)

// GameEnd is a bot's game-end callback: it gets the end of a game the bot
// played, with the final board, the bot's last memory and a link to the
// game's demo, which DownloadDemo downloads. ctx ends after GameEndTimeout,
// and Logger(ctx) writes to the console, never to the game log, since the
// game is over. An error, or a panic, is logged and changes nothing.
//
// Give it to Main with OnGameEnd, or to a Client with WithGameEndedDelivery.
type GameEnd func(ctx context.Context, ended GameEndedDelivery) error

// GameEndTimeout is how long serve, worker and run let a game-end callback
// take before they cancel its ctx and stop waiting for it. The server's
// runners wait only 5 s for a worker's answer, so an uploaded bot has that
// long.
const GameEndTimeout = time.Minute

// demoDownloadTimeout bounds one demo download. A long game with full logs
// is tens of megabytes, packed with gzip.
const demoDownloadTimeout = 2 * time.Minute

// demoHTTPClient downloads demos.
var demoHTTPClient = &http.Client{Timeout: demoDownloadTimeout}

// DecodeGameEndedDelivery reads a GameEndedDelivery from JSON, as the server
// and game clients send it. Every field of the contract must be there,
// though memory may be null; the outcome's reason must be one the SDK
// knows, and the generation is checked as DecodeGeneration checks it.
// Unknown fields are ignored.
func DecodeGameEndedDelivery(data []byte) (GameEndedDelivery, error) {
	var ended GameEndedDelivery
	if err := decodeStrict(data, &ended, "game_ended"); err != nil {
		return GameEndedDelivery{}, err
	}
	switch {
	case ended.GameID < 1:
		return GameEndedDelivery{}, errors.New("game_id must be at least 1")
	case ended.TeamColor < 1:
		return GameEndedDelivery{}, errors.New("team_color must be at least 1")
	case ended.TurnNumber < 1:
		return GameEndedDelivery{}, errors.New("turn_number must be at least 1")
	case ended.Demo.URL == "":
		return GameEndedDelivery{}, errors.New("demo.url is empty")
	}
	if err := ended.Outcome.validate(); err != nil {
		return GameEndedDelivery{}, err
	}
	if err := checkMemoryObject(ended.Memory); err != nil {
		return GameEndedDelivery{}, err
	}
	if err := validateGeneration(ended.Generation, "generation"); err != nil {
		return GameEndedDelivery{}, err
	}
	return ended, nil
}

// runGameEnd calls a game-end callback with a ctx that carries the bot's
// logger, which writes to outputs only, and ends after limit. It stops
// waiting then: a callback that ignores ctx keeps its goroutine until it
// returns. A panic is an error with its stack.
func runGameEnd(ctx context.Context, gameEnd GameEnd, ended GameEndedDelivery, outputs *logOutputs, limit time.Duration) error {
	// The turn log is closed from the start: the game has no turn left to
	// log to.
	logging := newTurnLog(newGameLog(ended.Generation.ID, 0), outputs)
	logging.close()
	ctx, cancel := context.WithTimeout(logging.context(ctx), limit)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- callGameEnd(ctx, gameEnd, ended) }()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		select {
		case err := <-done:
			return err
		default:
		}
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return fmt.Errorf("the game-end callback took longer than %s", limit)
		}
		return ctx.Err()
	}
}

// callGameEnd calls a game-end callback, and reports a panic in it as an
// error with its stack.
func callGameEnd(ctx context.Context, gameEnd GameEnd, ended GameEndedDelivery) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("panic: %v\n\n%s", recovered, debug.Stack())
		}
	}()
	return gameEnd(ctx, ended)
}

// DownloadDemo downloads the game's demo from link, a GameEndedDelivery's
// Demo, and returns the Demo file's JSON, which the 3D client's demo viewer
// and island-battles-mcp open. It is a plain GET of link.URL, which carries
// its own read-only token; the server packs the demo with gzip, and
// DownloadDemo unpacks it. A long game with full logs is tens of megabytes:
// WriteDemo writes it to a file instead of memory.
//
// Its errors are *GameError values, as a Client's: an expired or altered
// link is ErrAuthentication (401), a deleted game ErrRejected (404), and a
// download that gets no answer ErrTransport. They never hold the link's
// token.
func DownloadDemo(ctx context.Context, link DemoLink) ([]byte, error) {
	var demo bytes.Buffer
	if err := WriteDemo(ctx, link, &demo); err != nil {
		return nil, err
	}
	return demo.Bytes(), nil
}

// WriteDemo downloads the game's demo from link, as DownloadDemo does, and
// writes the Demo file's JSON, unpacked, to writer. The download takes at
// most 2 minutes, or until ctx is done. On an error, writer may hold part of
// the file.
func WriteDemo(ctx context.Context, link DemoLink, writer io.Writer) error {
	parsed, err := url.Parse(link.URL)
	if err != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.Host == "" {
		return newGameError(ErrProtocol, 0, "the demo link is not an http or https URL")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, link.URL, nil)
	if err != nil {
		return newGameError(ErrProtocol, 0, "the demo link is not a URL")
	}
	request.Header.Set("Accept", "application/json")
	// Asking for gzip ourselves means unpacking it ourselves, which works
	// with any http.RoundTripper.
	request.Header.Set("Accept-Encoding", "gzip")
	response, err := demoHTTPClient.Do(request)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return newGameError(ErrTransport, 0, "could not download the demo: "+withoutURL(err).Error())
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode >= 400 {
		data, _ := io.ReadAll(io.LimitReader(response.Body, 1<<20))
		if strings.EqualFold(response.Header.Get("Content-Encoding"), "gzip") {
			if unpacked, err := gzip.NewReader(bytes.NewReader(data)); err == nil {
				data, _ = io.ReadAll(io.LimitReader(unpacked, 1<<20))
			}
		}
		var answer problem
		_ = json.Unmarshal(data, &answer)
		return responseError(response.StatusCode, answer.Type, "could not download the demo: "+problemDetail(answer, response.StatusCode))
	}
	body := io.Reader(response.Body)
	if strings.EqualFold(response.Header.Get("Content-Encoding"), "gzip") {
		unpacked, err := gzip.NewReader(response.Body)
		if err != nil {
			return newGameError(ErrProtocol, 0, "the demo is not valid gzip: "+err.Error())
		}
		defer func() { _ = unpacked.Close() }()
		body = unpacked
	}
	if _, err := io.Copy(writer, body); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return newGameError(ErrTransport, 0, "could not download the demo: "+withoutURL(err).Error())
	}
	return nil
}

// problemDetail is an error response's detail, or its status text.
func problemDetail(answer problem, status int) string {
	if answer.Detail != "" {
		return answer.Detail
	}
	return http.StatusText(status)
}

// withoutURL drops the request URL from an HTTP client's error, so that the
// demo link's token never reaches a log.
func withoutURL(err error) error {
	var urlError *url.Error
	if errors.As(err, &urlError) {
		return urlError.Err
	}
	return err
}
