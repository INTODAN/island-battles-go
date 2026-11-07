// Package bots holds the bots the backend plays itself. Every bot is
// stateless: each turn it gets a models.BotTurn, with the memory it returned
// last turn, and answers with a models.BotTurnResult. The server stores the
// memory between turns, so a bot keeps nothing of its own.
//
// The default bots are numbered levels. They are deterministic: the same turn
// always gives the same result, so they use no randomness at all.
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

// defaultBot is a default bot with its catalog entry.
type defaultBot struct {
	models.DefaultBotView
	bot Bot
}

// defaults are the default bots, in level order.
var defaults = []defaultBot{
	{models.DefaultBotView{Level: 1, Name: "Level 1",
		Description: "Sends one soldier at a time from its first castle that has soldiers to the first enemy castle, like the example player."}, LevelOne{}},
	{models.DefaultBotView{Level: 2, Name: "Level 2",
		Description: "Attacks the nearest castle it can take with its free soldiers, counting how the defenders grow on the way; otherwise waits and grows."}, LevelTwo{}},
	{models.DefaultBotView{Level: 3, Name: "Level 3",
		Description: "Plays like level 2, and also keeps enough soldiers home against incoming troops, sends help to threatened castles, buys speed upgrades and times attacks from several castles to arrive together."}, LevelThree{}},
}

// Defaults lists the default bots in level order.
func Defaults() []models.DefaultBotView {
	catalog := make([]models.DefaultBotView, 0, len(defaults))
	for _, entry := range defaults {
		catalog = append(catalog, entry.DefaultBotView)
	}
	return catalog
}

// ForLevel returns the default bot of a level, and false for a level that
// has none.
func ForLevel(level uint) (Bot, bool) {
	for _, entry := range defaults {
		if entry.Level == level {
			return entry.bot, true
		}
	}
	return nil, false
}
