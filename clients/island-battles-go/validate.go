package islandbattles

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"unicode/utf8"
)

// ErrInvalidResult is the error of a result that breaks the contract's rules,
// which the server would reject whole.
var ErrInvalidResult = errors.New("invalid result")

// ValidateResult checks a result against the rules the server applies to its
// shape: at most MaxCommands commands, each a known type with valid
// parameters; at most MaxLogLines log lines, each with a known level, a
// logger of at most MaxLoggerLength characters and a message of 1 to
// MaxLogMessageLength; and memory that is a JSON object or null of at most
// MemoryLimitBytes. The error wraps ErrMemoryTooLarge for too much memory and
// ErrInvalidResult for anything else.
//
// The server also checks the commands against the board, for example that a
// castle has the soldiers it sends; ValidateResult can't.
func ValidateResult(result Result) error {
	if len(result.Commands) > MaxCommands {
		return fmt.Errorf("%w: %d commands; the limit is %d", ErrInvalidResult, len(result.Commands), MaxCommands)
	}
	for index, command := range result.Commands {
		if err := validateCommand(command); err != nil {
			return fmt.Errorf("%w: commands[%d]: %w", ErrInvalidResult, index, err)
		}
	}
	if len(result.Logs) > MaxLogLines {
		return fmt.Errorf("%w: %d log lines; the limit is %d", ErrInvalidResult, len(result.Logs), MaxLogLines)
	}
	for index, line := range result.Logs {
		if err := validateLogLine(line); err != nil {
			return fmt.Errorf("%w: logs[%d]: %w", ErrInvalidResult, index, err)
		}
	}
	return result.Memory.check()
}

func validateCommand(command Command) error {
	switch command.CommandType {
	case SendSoldierGroupCommand:
		var parameters SendSoldierGroupParameters
		if err := decodeParameters(command.Parameters, &parameters); err != nil {
			return err
		}
		if parameters.SoldierAmount < 1 {
			return errors.New("soldier_amount must be at least 1")
		}
	case UpgradeCastleCommand:
		var parameters UpgradeCastleParameters
		if err := decodeParameters(command.Parameters, &parameters); err != nil {
			return err
		}
		if !slices.Contains(UpgradeTypes(), parameters.UpgradeType) {
			return fmt.Errorf("unknown upgrade_type %q", parameters.UpgradeType)
		}
	default:
		return fmt.Errorf("unknown command_type %q", command.CommandType)
	}
	return nil
}

// decodeParameters decodes a command's parameters strictly: every field must
// be there, and no other.
func decodeParameters(raw json.RawMessage, target any) error {
	if err := requireFields(raw, reflect.TypeOf(target).Elem(), "parameters"); err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("parameters: %w", err)
	}
	return nil
}

func validateLogLine(line LogLine) error {
	if !slices.Contains(LogLevels(), line.Level) {
		return fmt.Errorf("unknown level %q", line.Level)
	}
	if !utf8.ValidString(line.Logger) || !utf8.ValidString(line.Message) {
		return errors.New("the logger and message must be UTF-8")
	}
	if length := utf8.RuneCountInString(line.Logger); length > MaxLoggerLength {
		return fmt.Errorf("the logger is %d characters; the limit is %d", length, MaxLoggerLength)
	}
	length := utf8.RuneCountInString(line.Message)
	if length == 0 {
		return errors.New("the message is empty")
	}
	if length > MaxLogMessageLength {
		return fmt.Errorf("the message is %d characters; the limit is %d (Log cuts it to fit)", length, MaxLogMessageLength)
	}
	return nil
}

// DecodeTurn reads a Turn from JSON, as a game client sends it. Every field
// of the contract must be there, though memory and turn_deadline_at may be
// null, and every soldier group's destination castle must be on the board.
// Unknown fields are ignored.
func DecodeTurn(data []byte) (Turn, error) {
	var turn Turn
	if err := json.Unmarshal(data, &turn); err != nil {
		return Turn{}, err
	}
	if err := requireFields(data, reflect.TypeFor[Turn](), "turn"); err != nil {
		return Turn{}, err
	}
	switch {
	case turn.GameID < 1:
		return Turn{}, errors.New("game_id must be at least 1")
	case turn.TeamColor < 1:
		return Turn{}, errors.New("team_color must be at least 1")
	case turn.Generation.ID < 1:
		return Turn{}, errors.New("generation.id must be at least 1")
	}
	if compact, err := turn.Memory.compact(); err != nil {
		return Turn{}, err
	} else if len(compact) > 0 && compact[0] != '{' {
		return Turn{}, errors.New("memory must be a JSON object or null")
	}
	for _, group := range turn.Generation.SoldierGroups {
		if _, found := group.DestinationCastle(turn.Generation); !found {
			return Turn{}, fmt.Errorf("soldier group %d's destination castle %d is missing", group.ID, group.DestinationCastleID)
		}
	}
	return turn, nil
}

// requireFields reports a JSON object, in data, that lacks a field of the
// struct type it decodes into, at any depth. A field may be null only when it
// is a pointer or Memory.
func requireFields(data []byte, structType reflect.Type, path string) error {
	var object map[string]json.RawMessage
	if err := json.Unmarshal(data, &object); err != nil || object == nil {
		return fmt.Errorf("%s must be a JSON object", path)
	}
	for index := range structType.NumField() {
		field := structType.Field(index)
		name, _, _ := strings.Cut(field.Tag.Get("json"), ",")
		if name == "" || name == "-" {
			continue
		}
		value, found := object[name]
		fieldPath := path + "." + name
		if !found {
			return fmt.Errorf("%s is missing", fieldPath)
		}
		if err := requireValue(value, field.Type, fieldPath); err != nil {
			return err
		}
	}
	return nil
}

func requireValue(value json.RawMessage, valueType reflect.Type, path string) error {
	isNull := bytes.Equal(bytes.TrimSpace(value), []byte("null"))
	switch {
	case valueType == reflect.TypeFor[Memory]() || valueType == reflect.TypeFor[json.RawMessage]() || valueType.Kind() == reflect.Pointer:
		return nil
	case isNull:
		return fmt.Errorf("%s must not be null", path)
	case valueType.Kind() == reflect.Struct && valueType.PkgPath() == reflect.TypeFor[Turn]().PkgPath():
		return requireFields(value, valueType, path)
	case valueType.Kind() == reflect.Slice && valueType.Elem().Kind() == reflect.Struct:
		var items []json.RawMessage
		if err := json.Unmarshal(value, &items); err != nil {
			return fmt.Errorf("%s must be an array", path)
		}
		for index, item := range items {
			if err := requireValue(item, valueType.Elem(), fmt.Sprintf("%s[%d]", path, index)); err != nil {
				return err
			}
		}
	}
	return nil
}
