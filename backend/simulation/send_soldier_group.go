package simulation

import (
	"fmt"

	"islandBattles/backend/models"
)

type SendSoldierGroupParams struct {
	OriginPersistentCastleID      uint `json:"origin_castle_id"`
	DestinationPersistentCastleID uint `json:"destination_castle_id"`
	SoldierAmount                 uint `json:"soldier_amount"`
}

// apply spawns a group at the origin castle with the game's next persistent ID
// and a snapshot of the castle's upgrades.
func (parameters SendSoldierGroupParams) apply(state *GameState, events []Event) ([]Event, error) {
	if parameters.SoldierAmount == 0 {
		return events, fmt.Errorf("soldier amount must be greater than zero")
	}
	if parameters.OriginPersistentCastleID == parameters.DestinationPersistentCastleID {
		return events, fmt.Errorf("origin and destination castles must differ")
	}
	originCastle, err := state.castle(parameters.OriginPersistentCastleID)
	if err != nil {
		return events, err
	}
	destinationCastle, err := state.castle(parameters.DestinationPersistentCastleID)
	if err != nil {
		return events, err
	}
	if originCastle.Soldiers < parameters.SoldierAmount {
		return events, fmt.Errorf("castle doesn't have enough soldier amount")
	}
	upgrades := originCastle.Upgrades.Clone()
	if err := upgrades.Validate(); err != nil {
		return events, err
	}
	originCastle.Soldiers -= parameters.SoldierAmount
	state.SoldierGroups[state.NextSoldierGroupID] = &SoldierGroup{
		ID:                  state.NextSoldierGroupID,
		TeamColor:           originCastle.TeamColor,
		Soldiers:            parameters.SoldierAmount,
		Position:            originCastle.Position,
		OriginCastleID:      originCastle.ID,
		DestinationCastleID: destinationCastle.ID,
		ClashRadius:         models.DefaultClashRadius,
		Upgrades:            upgrades,
	}
	state.NextSoldierGroupID++
	return events, nil
}
