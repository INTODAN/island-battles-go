package simulation

import (
	"fmt"

	"islandBattles/backend/models"
)

type IncreaseSoldierAmountParams struct{}

// apply is a generation's production. Every castle that a team owns produces
// its generation_amount. A castle that bursts this turn gets its production
// plus the burst, checked against its burst potential with every upgrade of
// the turn, and the burst becomes its debt. Otherwise the production repays
// any debt first, and what is left over grows the castle. A neutral castle
// produces nothing and keeps its soldiers until a team captures it. The step
// runs before the turn's arrivals, so a castle captured this turn was still
// neutral here: it produces from the next turn on. No castle becomes neutral
// again, so nothing else stops a castle from growing.
func (IncreaseSoldierAmountParams) apply(state *GameState, events []Event) ([]Event, error) {
	for _, castle := range state.SortedCastles() {
		if castle.OrderedBurst > 0 {
			potential, err := castle.Upgrades.Value(models.BurstPotentialUpgrade)
			if err != nil {
				return events, err
			}
			if float64(castle.OrderedBurst) > potential {
				return events, fmt.Errorf("%w: castle %d orders a burst of %d, above its burst potential of %v", models.ErrBurstAbovePotential, castle.ID, castle.OrderedBurst, potential)
			}
		}
		if castle.TeamColor == models.NeutralTeamColor {
			castle.OrderedBurst = 0
			continue
		}
		production, err := castle.Upgrades.Value(models.GenerationAmountUpgrade)
		if err != nil {
			return events, err
		}
		castle.produce(uint(production))
	}
	return events, nil
}

// produce adds one generation's production to the castle, as
// IncreaseSoldierAmountParams describes. A castle bursts only without debt,
// so a burst never meets an older debt.
func (castle *Castle) produce(production uint) {
	if castle.OrderedBurst > 0 {
		castle.Soldiers += production + castle.OrderedBurst
		castle.BurstDebt, castle.OrderedBurst = castle.OrderedBurst, 0
		return
	}
	repaid := min(castle.BurstDebt, production)
	castle.BurstDebt -= repaid
	castle.Soldiers += production - repaid
}
