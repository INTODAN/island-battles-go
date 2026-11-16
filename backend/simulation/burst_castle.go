package simulation

import (
	"fmt"

	"islandBattles/backend/models"
)

type BurstCastleParams struct {
	PersistentCastleID uint `json:"castle_id"`
	SoldierAmount      uint `json:"soldier_amount"`
}

// apply orders a burst. The turn's IncreaseSoldierAmount checks the amount
// against the castle's burst potential, counting every upgrade of the turn,
// and makes it the pending burst, which the next turn's production pays out.
// A castle bursts only when it has no debt, no pending burst and no other
// burst this turn.
func (parameters BurstCastleParams) apply(state *GameState, events []Event) ([]Event, error) {
	castle, err := state.castle(parameters.PersistentCastleID)
	if err != nil {
		return events, err
	}
	if parameters.SoldierAmount == 0 {
		return events, fmt.Errorf("%w: castle %d bursts no soldiers", models.ErrInvalidBurst, castle.ID)
	}
	if castle.BurstDebt > 0 || castle.PendingBurst > 0 || castle.OrderedBurst > 0 {
		return events, fmt.Errorf("%w: castle %d has a burst debt of %d, a pending burst of %d and a burst of %d ordered this turn", models.ErrBurstUnavailable, castle.ID, castle.BurstDebt, castle.PendingBurst, castle.OrderedBurst)
	}
	castle.OrderedBurst = parameters.SoldierAmount
	return events, nil
}
