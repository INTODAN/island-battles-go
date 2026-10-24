package simulation

type IncreaseSoldierAmountParams struct{}

// apply grows every castle, neutral ones included, by one soldier.
func (IncreaseSoldierAmountParams) apply(state *GameState, events []Event) ([]Event, error) {
	const amount = 1 // TODO: implement upgrade system and get amount from castles.
	for _, castle := range state.Castles {
		castle.Soldiers += amount
	}
	return events, nil
}
