package simulation

type MoveSoldierGroupsParams struct{}

// apply moves every group, resolves the clashes on the way and records each one
// as a ClashEvent. Defeated groups leave the field.
func (MoveSoldierGroupsParams) apply(state *GameState, events []Event) ([]Event, error) {
	soldierGroupsPositions, err := calculateInitialPositions(state)
	if err != nil {
		return events, err
	}
	clashes, err := calculateClashPositions(soldierGroupsPositions)
	if err != nil {
		return events, err
	}
	for _, clash := range clashEvents(clashes, uint(len(Clashes(events)))+1) {
		events = append(events, ClashEvent{Clash: clash})
	}

	for _, clash := range clashes {
		for _, participant := range clash.Participants {
			participant.SoldierGroup.Soldiers = participant.SoldiersAfter
			if participant.SoldiersAfter == 0 {
				delete(state.SoldierGroups, participant.SoldierGroup.ID)
			}
		}
	}
	// Store the resolved movement and the remaining pause for future generations.
	for _, movement := range soldierGroupsPositions {
		if movement.SoldierGroup.Soldiers == 0 {
			continue
		}
		movement.SoldierGroup.Position = movement.End
		movement.SoldierGroup.MovementPauseGenerations = movement.PauseGenerations
	}
	return events, nil
}
