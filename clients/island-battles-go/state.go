package islandbattles

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"time"
)

// The types in this file are the player API's messages that a Client sends
// and reads (backend/api/openapi.json), written by hand. A test keeps them
// equal to the document.

// OutcomeReason says why a game ended.
type OutcomeReason string

// The reasons a game ends.
const (
	// LastTeamStanding: one team was left after every other team lost all
	// its castles and soldier groups.
	LastTeamStanding OutcomeReason = "last_team_standing"
	// AllEliminated: the last teams were eliminated in the same turn, a draw.
	AllEliminated OutcomeReason = "all_eliminated"
	// GenerationCap: the game reached its last generation; the team with the
	// most castles won, and equal counts are a draw.
	GenerationCap OutcomeReason = "generation_cap"
	// TurnTimeout: a turn passed its deadline; every team with a player who
	// hadn't completed it lost.
	TurnTimeout OutcomeReason = "turn_timeout"
)

// GameOutcome is how a game ended. It is the contract's GameOutcome.
type GameOutcome struct {
	Reason OutcomeReason `json:"reason"`
	// WinnerTeamColor is the winning team, or nil for a draw.
	WinnerTeamColor *uint `json:"winner_team_color"`
	// Summary is the outcome in one sentence, naming the winning team.
	Summary string `json:"summary"`
}

// botChoice is who plays a seat: the player's own bot (localhost), or a
// default bot that the backend plays. It is the contract's BotChoice.
type botChoice struct {
	Kind  string `json:"kind"`
	Level *uint  `json:"level,omitempty"`
}

// generationState is the player's view of the game's current generation:
// GET /game/from-lobby/{lobby_id}/generation, and the state of the
// GenerationEvent and GameEndedEvent pushed on the player's channel. It is
// the contract's GenerationState.
type generationState struct {
	Schema         string       `json:"$schema,omitempty"`
	GameID         uint         `json:"game_id"`
	LobbyID        string       `json:"lobby_id"`
	LobbyRound     int64        `json:"lobby_round"`
	GenerationID   uint         `json:"generation_id"`
	PlayerID       string       `json:"player_id"`
	TeamColor      uint         `json:"team_color"`
	Channel        string       `json:"channel"`
	Submitted      bool         `json:"submitted"`
	Generation     Generation   `json:"generation"`
	LogLinesStored uint         `json:"log_lines_stored"`
	Memory         Memory       `json:"memory"`
	Bot            botChoice    `json:"bot"`
	TurnDeadlineAt *time.Time   `json:"turn_deadline_at"`
	Outcome        *GameOutcome `json:"outcome,omitempty"`
}

// generationEvent is the GenerationEvent and the GameEndedEvent, which the
// server publishes on the player's channel.
type generationEvent struct {
	SchemaVersion int             `json:"schema_version"`
	Type          string          `json:"type"`
	State         json.RawMessage `json:"state"`
}

// The event types of the player's channel.
const (
	nextGenerationEvent = "game.next_generation"
	gameEndedEvent      = "game.ended"
)

// registerCommands is a batch of commands for one generation: POST
// /generation/commands. It is the contract's RegisterGenerationCommands.
type registerCommands struct {
	Schema       string    `json:"$schema,omitempty"`
	GameID       uint      `json:"game_id"`
	GenerationID uint      `json:"generation_id"`
	RequestID    string    `json:"request_id"`
	Complete     bool      `json:"complete"`
	Commands     []Command `json:"commands"`
	Memory       Memory    `json:"memory,omitempty"`
	Logs         []LogLine `json:"logs,omitempty"`
}

// connectionToken is POST /lobby/v1/connection-token's answer: a Centrifugo
// connection token and where to connect with it.
type connectionToken struct {
	Schema       string    `json:"$schema,omitempty"`
	Token        string    `json:"token"`
	ExpiresAt    time.Time `json:"expires_at"`
	WebsocketURL string    `json:"websocket_url"`
}

// upgradeTable is GET /game/upgrades's answer.
type upgradeTable struct {
	Schema   string    `json:"$schema,omitempty"`
	Upgrades []Upgrade `json:"upgrades"`
}

// problem is the part of an error response the client reads.
type problem struct {
	Type   string `json:"type"`
	Detail string `json:"detail"`
}

// decodeStrict decodes JSON into target, a pointer to one of this package's
// structs, and requires every field its tags don't mark omitempty.
func decodeStrict(data []byte, target any, path string) error {
	if err := json.Unmarshal(data, target); err != nil {
		return err
	}
	return requireFields(data, reflect.TypeOf(target).Elem(), path)
}

// parseState reads and checks a generation state.
func parseState(data []byte) (generationState, error) {
	var state generationState
	if err := decodeStrict(data, &state, "state"); err != nil {
		return generationState{}, protocolError("invalid generation state", err)
	}
	if err := state.validate(); err != nil {
		return generationState{}, protocolError("invalid generation state", err)
	}
	return state, nil
}

func (state generationState) validate() error {
	switch {
	case state.GameID < 1:
		return errors.New("game_id must be at least 1")
	case state.GenerationID < 1:
		return errors.New("generation_id must be at least 1")
	case state.TeamColor < 1:
		return errors.New("team_color must be at least 1")
	case state.Bot.Kind != "localhost" && state.Bot.Kind != "default":
		return fmt.Errorf("unknown bot kind %q", state.Bot.Kind)
	}
	if _, err := canonicalUUID(state.LobbyID); err != nil {
		return fmt.Errorf("lobby_id: %w", err)
	}
	if _, err := canonicalUUID(state.PlayerID); err != nil {
		return fmt.Errorf("player_id: %w", err)
	}
	if err := checkMemoryObject(state.Memory); err != nil {
		return err
	}
	if state.Outcome != nil {
		switch state.Outcome.Reason {
		case LastTeamStanding, AllEliminated, GenerationCap, TurnTimeout:
		default:
			return fmt.Errorf("unknown outcome reason %q", state.Outcome.Reason)
		}
	}
	return validateGeneration(state.Generation, "generation")
}

// parseEvent reads a publication of the player's channel: a GenerationEvent,
// or a GameEndedEvent with an outcome.
func parseEvent(data []byte) (generationState, error) {
	var event generationEvent
	if err := json.Unmarshal(data, &event); err != nil {
		return generationState{}, protocolError("invalid event", err)
	}
	if event.SchemaVersion != 1 || (event.Type != nextGenerationEvent && event.Type != gameEndedEvent) {
		return generationState{}, protocolError("invalid event", fmt.Errorf("unknown event %q of schema version %d", event.Type, event.SchemaVersion))
	}
	state, err := parseState(event.State)
	if err != nil {
		return generationState{}, err
	}
	if event.Type == gameEndedEvent && state.Outcome == nil {
		return generationState{}, newGameError(ErrProtocol, 0, "game.ended without an outcome")
	}
	return state, nil
}

func protocolError(detail string, err error) *GameError {
	return newGameError(ErrProtocol, 0, fmt.Sprintf("%s: %v", detail, err))
}

// canonicalUUID checks a UUID's text form and returns it in lower case.
func canonicalUUID(text string) (string, error) {
	if len(text) != 36 {
		return "", fmt.Errorf("%q is not a UUID", text)
	}
	canonical := []byte(text)
	for index, character := range canonical {
		switch {
		case index == 8 || index == 13 || index == 18 || index == 23:
			if character != '-' {
				return "", fmt.Errorf("%q is not a UUID", text)
			}
		case character >= '0' && character <= '9', character >= 'a' && character <= 'f':
		case character >= 'A' && character <= 'F':
			canonical[index] = character + 'a' - 'A'
		default:
			return "", fmt.Errorf("%q is not a UUID", text)
		}
	}
	return string(canonical), nil
}
