package simulation

import "islandBattles/backend/models"

type IncreaseSoldierAmountParams struct{}

// apply grows every castle that a team owns by one soldier. A neutral castle
// keeps its soldiers until a team captures it. The step runs before the turn's
// arrivals, so a castle captured this turn was still neutral here: it grows
// from the next turn on. No castle becomes neutral again, so nothing else
// stops a castle from growing.
func (IncreaseSoldierAmountParams) apply(state *GameState, events []Event) ([]Event, error) {
	const amount = 1 // TODO: implement upgrade system and get amount from castles.
	for _, castle := range state.Castles {
		if castle.TeamColor == models.NeutralTeamColor {
			continue
		}
		castle.Soldiers += amount
	}
	return events, nil
}
