package simulation

type RemoveArrivedSoldierGroupsParams struct{}

// apply lands every group that reached its destination castle, in persistent ID
// order, so each arrival meets the castle as the earlier arrivals left it. A
// friendly arrival reinforces the castle; a hostile one fights its garrison and
// captures it when more attackers than defenders arrive.
func (RemoveArrivedSoldierGroupsParams) apply(state *GameState, events []Event) ([]Event, error) {
	for _, soldierGroup := range state.SortedSoldierGroups() {
		destination, err := state.castle(soldierGroup.DestinationCastleID)
		if err != nil {
			return events, err
		}
		if soldierGroup.Position != destination.Position {
			continue
		}
		switch {
		case destination.TeamColor == soldierGroup.TeamColor:
			destination.Soldiers += soldierGroup.Soldiers
		case soldierGroup.Soldiers > destination.Soldiers:
			destination.TeamColor = soldierGroup.TeamColor
			destination.Soldiers = soldierGroup.Soldiers - destination.Soldiers
		default:
			destination.Soldiers -= soldierGroup.Soldiers
		}
		delete(state.SoldierGroups, soldierGroup.ID)
	}
	return events, nil
}
