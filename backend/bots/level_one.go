package bots

import (
	"context"

	"islandBattles/backend/models"
)

// LevelOne is the example player's strategy: each turn it sends one soldier
// from its first castle with soldiers to the first enemy castle, in ID order.
// It never attacks neutral castles and keeps no memory.
type LevelOne struct{}

func (LevelOne) Play(_ context.Context, turn models.BotTurn) (models.BotTurnResult, error) {
	board := newBoard(turn)
	result := models.BotTurnResult{Commands: []models.PublicGenerationCommand{}}
	var origin, enemy *models.CastleView
	for index, castle := range board.castles {
		if origin == nil && castle.TeamColor == board.team && castle.Soldiers > 0 {
			origin = &board.castles[index]
		}
		if enemy == nil && castle.TeamColor != models.NeutralTeamColor && castle.TeamColor != board.team {
			enemy = &board.castles[index]
		}
	}
	switch {
	case origin == nil:
		result.Logs = []models.GenerationLogLine{logLine(1, "No castle of mine has a soldier to send; passing.")}
	case enemy == nil:
		result.Logs = []models.GenerationLogLine{logLine(1, "No enemy castle is left; passing.")}
	default:
		result.Commands = append(result.Commands, sendCommand(origin.ID, enemy.ID, 1))
		result.Logs = []models.GenerationLogLine{logLine(1, "Sending 1 soldier from castle %d to enemy castle %d.", origin.ID, enemy.ID)}
	}
	return result, nil
}
