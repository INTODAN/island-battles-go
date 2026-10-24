package simulation

import (
	"encoding/json"
	"fmt"

	"islandBattles/backend/models"
)

// Command is one rule applied to the state, such as a player's order or a step
// every turn takes. Build commands with DecodeCommand.
type Command interface {
	// apply changes state in place and appends what happened to events.
	apply(state *GameState, events []Event) ([]Event, error)
}

// Event is something a turn produced that is published with its generation.
type Event interface {
	isEvent()
}

// ClashEvent records one resolved clash.
type ClashEvent struct {
	Clash models.Clash
}

func (ClashEvent) isEvent() {}

// Simulate applies the commands in order to a copy of state and returns the
// resulting state with the events the commands produced. state is unchanged.
// A rejected command rejects the whole simulation.
func Simulate(state GameState, commands []Command) (GameState, []Event, error) {
	next := state.Clone()
	events := []Event{}
	for _, command := range commands {
		var err error
		events, err = command.apply(&next, events)
		if err != nil {
			return GameState{}, nil, err
		}
	}
	return next, events, nil
}

// Clashes lists the clash events in the order they happened.
func Clashes(events []Event) []models.Clash {
	clashes := []models.Clash{}
	for _, event := range events {
		if clashEvent, isClash := event.(ClashEvent); isClash {
			clashes = append(clashes, clashEvent.Clash)
		}
	}
	return clashes
}

// EndOfTurnCommands run after the players' commands in every turn: groups move
// and clash, castles grow, then groups that reached their castle arrive.
func EndOfTurnCommands() []models.CommandFields {
	return []models.CommandFields{
		{CommandType: models.MoveSoldierGroups, Parameters: []byte("{}")},
		{CommandType: models.IncreaseSoldierAmount, Parameters: []byte("{}")},
		{CommandType: models.RemoveArrivedSoldierGroups, Parameters: []byte("{}")},
	}
}

// DecodeCommands decodes stored commands, keeping their order.
func DecodeCommands(commands []models.CommandFields) ([]Command, error) {
	decoded := make([]Command, 0, len(commands))
	for _, command := range commands {
		next, err := DecodeCommand(command)
		if err != nil {
			return nil, err
		}
		decoded = append(decoded, next)
	}
	return decoded, nil
}

func DecodeCommand(command models.CommandFields) (Command, error) {
	switch command.CommandType {
	case models.UpgradeCastle:
		return decodeParameters[UpgradeCastleParams](command.Parameters)
	case models.IncreaseSoldierAmount:
		return decodeParameters[IncreaseSoldierAmountParams](command.Parameters)
	case models.MoveSoldierGroups:
		return decodeParameters[MoveSoldierGroupsParams](command.Parameters)
	case models.SendSoldierGroup:
		return decodeParameters[SendSoldierGroupParams](command.Parameters)
	case models.RemoveArrivedSoldierGroups:
		return decodeParameters[RemoveArrivedSoldierGroupsParams](command.Parameters)
	}
	return nil, fmt.Errorf("unknown command type %q", command.CommandType)
}

func decodeParameters[Parameters Command](raw []byte) (Command, error) {
	var parameters Parameters
	if err := json.Unmarshal(raw, &parameters); err != nil {
		return nil, err
	}
	return parameters, nil
}
