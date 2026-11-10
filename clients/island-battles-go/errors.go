package islandbattles

import (
	"errors"
	"fmt"
)

// The kinds of error a Client reports. Each error the Client returns is a
// *GameError whose Kind is one of these, so test for them with errors.Is,
// and read the HTTP status with errors.As:
//
//	var gameError *islandbattles.GameError
//	if errors.Is(err, islandbattles.ErrInsufficientSoldiers) && errors.As(err, &gameError) {
//		fmt.Println(gameError.Status, gameError.Detail)
//	}
//
// A result the bot returns that breaks the contract's rules is reported, as
// by ValidateResult, with ErrInvalidResult or ErrMemoryTooLarge.
var (
	// ErrAuthentication: the server refused the credential (HTTP 401).
	ErrAuthentication = errors.New("authentication failed")
	// ErrTransport: a request got no answer, even when retried, or the
	// server answered 429 or 5xx.
	ErrTransport = errors.New("transport error")
	// ErrProtocol: the server answered something the client can't read.
	ErrProtocol = errors.New("protocol error")
	// ErrClientClosed: the client was closed.
	ErrClientClosed = errors.New("client closed")
	// ErrCastleNotFound: a command names a castle that isn't in the
	// generation.
	ErrCastleNotFound = errors.New("castle not found")
	// ErrCastleNotOwned: a command's castle belongs to another team or to
	// none.
	ErrCastleNotOwned = errors.New("castle not owned")
	// ErrUpgradeMaxLevel: an upgrade type is at its maximum level, counting
	// this turn's upgrades.
	ErrUpgradeMaxLevel = errors.New("upgrade at its maximum level")
	// ErrInsufficientSoldiers: a castle has too few soldiers left after this
	// turn's reservations.
	ErrInsufficientSoldiers = errors.New("insufficient soldiers")
	// ErrGameEnded: the server refused a batch because the game has ended.
	// The Client handles it itself: it reads the final state and reports
	// the end.
	ErrGameEnded = errors.New("game ended")
	// ErrGameLogLimit: the server refused a batch's game log lines, which
	// would pass its limit. The Client handles it itself: it sends the
	// batch again without the lines.
	ErrGameLogLimit = errors.New("game log limit reached")
	// ErrRejected: the server refused a request for another reason (any
	// other 4xx status), or the seat can't be played.
	ErrRejected = errors.New("rejected")
)

// problemKinds are the server's problem types that the client tells apart:
// the command rejections, shared by every command, the game's end, the game
// log limit and the bot memory limit.
var problemKinds = map[string]error{
	"urn:island-battles:problem:game-ended":            ErrGameEnded,
	"urn:island-battles:problem:castle-not-found":      ErrCastleNotFound,
	"urn:island-battles:problem:castle-not-owned":      ErrCastleNotOwned,
	"urn:island-battles:problem:upgrade-max-level":     ErrUpgradeMaxLevel,
	"urn:island-battles:problem:insufficient-soldiers": ErrInsufficientSoldiers,
	"urn:island-battles:problem:game-log-limit":        ErrGameLogLimit,
	"urn:island-battles:problem:bot-memory-too-large":  ErrMemoryTooLarge,
}

// GameError is an error of the Client: its kind, the HTTP status when a
// response caused it, and the server's or client's detail. It never holds
// a credential or a response body.
type GameError struct {
	// Kind is one of the Err variables of this package, such as
	// ErrInsufficientSoldiers; errors.Is matches it.
	Kind error
	// Status is the HTTP status, or 0 when no response caused the error.
	Status int
	// Detail says what went wrong.
	Detail string
}

func (gameError *GameError) Error() string {
	if gameError.Status != 0 {
		return fmt.Sprintf("HTTP %d: %s", gameError.Status, gameError.Detail)
	}
	return gameError.Detail
}

// Unwrap returns the Kind, for errors.Is.
func (gameError *GameError) Unwrap() error { return gameError.Kind }

func newGameError(kind error, status int, detail string) *GameError {
	return &GameError{Kind: kind, Status: status, Detail: detail}
}

// responseError maps an HTTP error response onto a GameError: 401 is
// ErrAuthentication, 429 and 5xx are ErrTransport, a known problem type is
// its kind, and anything else is ErrRejected.
func responseError(status int, problemType, detail string) *GameError {
	if detail == "" {
		detail = fmt.Sprintf("request failed (%d)", status)
	}
	switch {
	case status == 401:
		return newGameError(ErrAuthentication, status, detail)
	case status == 429 || status >= 500:
		return newGameError(ErrTransport, status, detail)
	}
	if kind, found := problemKinds[problemType]; found {
		return newGameError(kind, status, detail)
	}
	return newGameError(ErrRejected, status, detail)
}
