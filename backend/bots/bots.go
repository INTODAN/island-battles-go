// Package bots holds the bots the backend plays itself. Every bot is
// stateless: each turn it gets a models.BotTurn, with the memory it returned
// last turn, and answers with a models.BotTurnResult. The server stores the
// memory between turns, so a bot keeps nothing of its own.
package bots

import (
	"context"

	"islandBattles/backend/models"
)

// Bot plays one turn. The result's commands and log lines follow the rules
// of POST /generation/commands, and its memory may be at most
// models.MaxBotMemoryBytes as compact JSON. An error passes the turn.
type Bot interface {
	Play(ctx context.Context, turn models.BotTurn) (models.BotTurnResult, error)
}
