package simulation

import (
	"fmt"

	"islandBattles/backend/models"
)

type UpgradeCastleParams struct {
	PersistentCastleID uint               `json:"castle_id"`
	UpgradeType        models.UpgradeType `json:"upgrade_type"`
}

// apply buys one level from the castle's current balance. A castle may pay
// exactly its balance, leaving no soldiers.
func (parameters UpgradeCastleParams) apply(state *GameState, events []Event) ([]Event, error) {
	castle, err := state.castle(parameters.PersistentCastleID)
	if err != nil {
		return events, err
	}
	upgrades, err := castle.Upgrades.Upgraded(parameters.UpgradeType)
	if err != nil {
		return events, err
	}
	cost, err := upgrades.Cost(parameters.UpgradeType)
	if err != nil {
		return events, err
	}
	if castle.Soldiers < cost {
		return events, fmt.Errorf("%w: %s level %d costs %d, castle has %d", models.ErrInsufficientSoldiers, parameters.UpgradeType, upgrades[parameters.UpgradeType], cost, castle.Soldiers)
	}
	castle.Upgrades = upgrades
	castle.Soldiers -= cost
	return events, nil
}
