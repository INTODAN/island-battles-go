package services

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"islandBattles/backend/models"
	"islandBattles/backend/simulation"
)

// turnReservations follows what the turn's accepted commands take from each
// castle: the soldiers that sends and upgrades claim, and every upgrade type's
// level after the upgrades bought so far. Simulate deducts the same totals in
// whatever order the players' commands run, and buys each castle's levels in
// the same sequence, so commands that fit here cannot fail there.
type turnReservations map[uint]*castleReservation

type castleReservation struct {
	castle     models.Castle
	unreserved uint
	levels     models.Upgrades
}

func newTurnReservations(generation *models.Generation) turnReservations {
	reservations := turnReservations{}
	for _, castle := range generation.Map.Castles {
		reservations[castle.PersistentCastleID] = &castleReservation{castle: castle, unreserved: castle.SoldierAmount, levels: castle.Upgrades.Clone()}
	}
	return reservations
}

// reservedTurn is the generation's reservations after every command already
// accepted this turn, by any player, as validateCommands checks a batch
// against them.
func reservedTurn(generation *models.Generation, inputs []models.GenerationInput) (turnReservations, error) {
	reservations := newTurnReservations(generation)
	for _, input := range inputs {
		var commands []models.CommandFields
		if err := json.Unmarshal(input.Commands, &commands); err != nil {
			return nil, err
		}
		for _, command := range commands {
			// %v rather than %w: a stored command that no longer fits is a
			// server fault, not a rejection of this batch.
			if err := reservations.reserveStored(command); err != nil {
				return nil, fmt.Errorf("persisted reservations do not fit the generation: %v", err)
			}
		}
	}
	return reservations, nil
}

func (reservations turnReservations) find(castleID uint) (*castleReservation, error) {
	reservation, exists := reservations[castleID]
	if !exists {
		return nil, &GenerationError{Status: 422, Type: CastleNotFoundProblemType, Detail: fmt.Sprintf("castle %d is not in this generation", castleID)}
	}
	return reservation, nil
}

// owned finds a castle that the player's team owns.
func (reservations turnReservations) owned(teamColor, castleID uint) (*castleReservation, error) {
	reservation, err := reservations.find(castleID)
	if err != nil {
		return nil, err
	}
	if owner := reservation.castle.TeamColor; owner != teamColor {
		description := fmt.Sprintf("team %d", owner)
		if owner == models.NeutralTeamColor {
			description = "no team"
		}
		return nil, &GenerationError{Status: 403, Type: CastleNotOwnedProblemType, Detail: fmt.Sprintf("castle %d belongs to %s, not to your team %d", castleID, description, teamColor)}
	}
	return reservation, nil
}

// reserveSend claims a group's soldiers from its origin castle.
func (reservations turnReservations) reserveSend(parameters simulation.SendSoldierGroupParams) error {
	origin, err := reservations.find(parameters.OriginPersistentCastleID)
	if err != nil {
		return err
	}
	if _, err := reservations.find(parameters.DestinationPersistentCastleID); err != nil {
		return err
	}
	if parameters.SoldierAmount > origin.unreserved {
		return &GenerationError{Status: 409, Type: InsufficientSoldiersProblemType, Detail: fmt.Sprintf("castle %d has %d unreserved soldiers; sending %d needs more", parameters.OriginPersistentCastleID, origin.unreserved, parameters.SoldierAmount)}
	}
	origin.unreserved -= parameters.SoldierAmount
	return nil
}

// reserveUpgrade buys the type's next level and claims its cost.
func (reservations turnReservations) reserveUpgrade(parameters simulation.UpgradeCastleParams) error {
	castle, err := reservations.find(parameters.PersistentCastleID)
	if err != nil {
		return err
	}
	upgraded, err := castle.levels.Upgraded(parameters.UpgradeType)
	if errors.Is(err, models.ErrUpgradeMaxLevel) {
		return &GenerationError{Status: 409, Type: UpgradeMaxLevelProblemType, Detail: fmt.Sprintf("castle %d already reaches %s level %d, the maximum, counting this turn's upgrades", parameters.PersistentCastleID, parameters.UpgradeType, castle.levels[parameters.UpgradeType])}
	}
	if err != nil {
		return err
	}
	cost, err := upgraded.Cost(parameters.UpgradeType)
	if err != nil {
		return err
	}
	if cost > castle.unreserved {
		return &GenerationError{Status: 409, Type: InsufficientSoldiersProblemType, Detail: fmt.Sprintf("castle %d has %d unreserved soldiers; %s level %d costs %d", parameters.PersistentCastleID, castle.unreserved, parameters.UpgradeType, upgraded[parameters.UpgradeType], cost)}
	}
	castle.levels = upgraded
	castle.unreserved -= cost
	return nil
}

// reserveStored replays a command accepted earlier this turn.
func (reservations turnReservations) reserveStored(command models.CommandFields) error {
	switch command.CommandType {
	case models.SendSoldierGroup:
		var parameters simulation.SendSoldierGroupParams
		if err := json.Unmarshal(command.Parameters, &parameters); err != nil {
			return err
		}
		return reservations.reserveSend(parameters)
	case models.UpgradeCastle:
		var parameters simulation.UpgradeCastleParams
		if err := json.Unmarshal(command.Parameters, &parameters); err != nil {
			return err
		}
		return reservations.reserveUpgrade(parameters)
	}
	return fmt.Errorf("unexpected stored command type %q", command.CommandType)
}

// acceptSend validates a player's send_soldier_group, reserves it and
// translates it into the internal command.
func (reservations turnReservations) acceptSend(teamColor uint, raw json.RawMessage) (models.CommandFields, error) {
	var parameters models.SendSoldierGroupParameters
	if !decodeStrictly(raw, &parameters) {
		return models.CommandFields{}, reject(422, "send_soldier_group parameters must contain integer castle IDs and soldier_amount")
	}
	if parameters.OriginCastleID == nil || parameters.DestinationCastleID == nil || parameters.SoldierAmount == nil || *parameters.SoldierAmount == 0 {
		return models.CommandFields{}, reject(422, "origin_castle_id, destination_castle_id and positive soldier_amount are required")
	}
	if *parameters.OriginCastleID == *parameters.DestinationCastleID {
		return models.CommandFields{}, reject(422, "origin and destination must differ")
	}
	// A missing destination is reported before an unowned origin.
	if _, err := reservations.find(*parameters.DestinationCastleID); err != nil {
		return models.CommandFields{}, err
	}
	if _, err := reservations.owned(teamColor, *parameters.OriginCastleID); err != nil {
		return models.CommandFields{}, err
	}
	internal := simulation.SendSoldierGroupParams{OriginPersistentCastleID: *parameters.OriginCastleID, DestinationPersistentCastleID: *parameters.DestinationCastleID, SoldierAmount: *parameters.SoldierAmount}
	if err := reservations.reserveSend(internal); err != nil {
		return models.CommandFields{}, err
	}
	stored, err := json.Marshal(internal)
	return models.CommandFields{CommandType: models.SendSoldierGroup, Parameters: stored}, err
}

// acceptUpgrade validates a player's upgrade_castle, reserves it and
// translates it into the internal command.
func (reservations turnReservations) acceptUpgrade(teamColor uint, raw json.RawMessage) (models.CommandFields, error) {
	var parameters models.UpgradeCastleParameters
	if !decodeStrictly(raw, &parameters) || parameters.CastleID == nil || parameters.UpgradeType == nil {
		return models.CommandFields{}, reject(422, "upgrade_castle parameters must contain an integer castle_id and a string upgrade_type")
	}
	if _, err := (models.Upgrades{}).Level(*parameters.UpgradeType); err != nil {
		return models.CommandFields{}, &GenerationError{Status: 422, Type: UnknownUpgradeTypeProblemType, Detail: fmt.Sprintf("upgrade_type %q is not one of %v", *parameters.UpgradeType, models.UpgradeTypes())}
	}
	if _, err := reservations.owned(teamColor, *parameters.CastleID); err != nil {
		return models.CommandFields{}, err
	}
	internal := simulation.UpgradeCastleParams{PersistentCastleID: *parameters.CastleID, UpgradeType: *parameters.UpgradeType}
	if err := reservations.reserveUpgrade(internal); err != nil {
		return models.CommandFields{}, err
	}
	stored, err := json.Marshal(internal)
	return models.CommandFields{CommandType: models.UpgradeCastle, Parameters: stored}, err
}

// decodeStrictly decodes one parameters object, refusing unknown keys and
// trailing data.
func decodeStrictly(raw json.RawMessage, parameters any) bool {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(parameters); err != nil {
		return false
	}
	var trailing any
	return errors.Is(decoder.Decode(&trailing), io.EOF)
}
