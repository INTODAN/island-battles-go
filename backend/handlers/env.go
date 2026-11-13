package handlers

import "gorm.io/gorm"

type Env struct {
	DB *gorm.DB
	// ServerBots says that the server offers hosted and uploaded bots
	// (ISLAND_BATTLES_SERVER_BOTS=on), which GET /bots/defaults reports.
	ServerBots bool
}
