package defaultbots

import (
	"context"

	islandbattles "github.com/INTODAN/island-battles-go/clients/island-battles-go"
)

// LevelOne is the example player's strategy: each turn it sends one soldier
// from its first castle with soldiers to the first enemy castle, in ID order.
// It never attacks neutral castles and keeps no memory.
func LevelOne(_ context.Context, turn islandbattles.Turn) (islandbattles.Result, error) {
	board := newBoard(turn)
	result := islandbattles.Result{Commands: []islandbattles.Command{}}
	var origin, enemy *islandbattles.Castle
	for index, castle := range board.castles {
		if origin == nil && castle.TeamColor == board.team && castle.Soldiers > 0 {
			origin = &board.castles[index]
		}
		if enemy == nil && castle.TeamColor != islandbattles.NeutralTeamColor && castle.TeamColor != board.team {
			enemy = &board.castles[index]
		}
	}
	switch {
	case origin == nil:
		result.Logs = []islandbattles.LogLine{logLine(1, "No castle of mine has a soldier to send; passing.")}
	case enemy == nil:
		result.Logs = []islandbattles.LogLine{logLine(1, "No enemy castle is left; passing.")}
	default:
		result.Commands = append(result.Commands, islandbattles.SendSoldierGroup(origin.ID, enemy.ID, 1))
		result.Logs = []islandbattles.LogLine{logLine(1, "Sending 1 soldier from castle %d to enemy castle %d.", origin.ID, enemy.ID)}
	}
	return result, nil
}
