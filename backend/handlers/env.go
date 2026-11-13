package handlers

import (
	"islandBattles/backend/services"

	"gorm.io/gorm"
)

type Env struct {
	DB *gorm.DB
	// ServerBots says that the server offers hosted and uploaded bots
	// (ISLAND_BATTLES_SERVER_BOTS=on), which GET /bots/defaults reports.
	ServerBots bool
	// BotCompute is the monthly guard on the bot runners' compute, which
	// GET /game/bot-compute reports. Nil reports an unmeasured month.
	BotCompute *services.BotComputeGuard
}
