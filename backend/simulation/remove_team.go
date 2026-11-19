package simulation

import (
	"encoding/json"
	"fmt"
	"slices"

	"islandBattles/backend/models"
)

// RemoveTeamParams takes a team out of a real-time game when it resigned or a
// player of it went silent. Every castle of the team turns neutral, keeping
// its soldiers and upgrades but not its burst debt, and every soldier group of
// the team is removed from the field at once, without a clash. The team then
// has no castles and no groups, so it is out, and Outcome treats it as
// eliminated. Only the server adds this command, first in a turn
// (TurnCommands); players can't send it.
type RemoveTeamParams struct {
	TeamColor uint `json:"team_color"`
}

func (params RemoveTeamParams) apply(state *GameState, events []Event) ([]Event, error) {
	if params.TeamColor == models.NeutralTeamColor {
		return events, fmt.Errorf("remove_team needs a team colour, not the neutral %d", models.NeutralTeamColor)
	}
	for _, castle := range state.Castles {
		if castle.TeamColor == params.TeamColor {
			castle.TeamColor = models.NeutralTeamColor
			castle.BurstDebt, castle.OrderedBurst = 0, 0
		}
	}
	for id, group := range state.SoldierGroups {
		if group.TeamColor == params.TeamColor {
			delete(state.SoldierGroups, id)
		}
	}
	return events, nil
}

// TeamBatch is the commands that one team's player stored for a turn.
type TeamBatch struct {
	Color    uint
	Commands []models.CommandFields
}

// TurnCommands lists a turn's commands before its end-of-turn steps. The teams
// in leaving leave first, in colour order, with RemoveTeam, so the turn's
// other commands meet their castles neutral; their own commands of the turn
// are dropped. The other batches follow in the order given. Without a leaving
// team it is the batches' commands, in order.
func TurnCommands(leaving []uint, batches []TeamBatch) ([]models.CommandFields, error) {
	commands := []models.CommandFields{}
	for _, color := range slices.Sorted(slices.Values(leaving)) {
		parameters, err := json.Marshal(RemoveTeamParams{TeamColor: color})
		if err != nil {
			return nil, err
		}
		commands = append(commands, models.CommandFields{CommandType: models.RemoveTeam, Parameters: parameters})
	}
	for _, batch := range batches {
		if !slices.Contains(leaving, batch.Color) {
			commands = append(commands, batch.Commands...)
		}
	}
	return commands, nil
}
