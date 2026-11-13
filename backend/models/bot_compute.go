package models

import "time"

// BotComputeUsage is the cached month-to-date compute of the bot runners
// (#226): the one row of island_bot_compute_usage, which the backend reads
// from CloudWatch at most once per cache period and keeps here for every
// Lambda instance. A new month replaces the row. It holds no game's data.
type BotComputeUsage struct {
	// Month is the first day of the month, at midnight UTC.
	Month     time.Time `gorm:"column:month;type:date;primaryKey"`
	GBSeconds float64   `gorm:"column:gb_seconds;not null"`
	ReadAt    time.Time `gorm:"column:read_at;not null"`
}

func (BotComputeUsage) TableName() string { return "island_bot_compute_usage" }

// BotComputeView is GET /game/bot-compute: the bot runners' compute this
// month against the monthly budget, which decides whether games with
// uploaded bots may start.
type BotComputeView struct {
	Month           string  `json:"month" pattern:"^[0-9]{4}-[0-9]{2}$" example:"2026-11" doc:"The month, in UTC, as YYYY-MM. The budget starts again on the first of the next month."`
	UsedGBSeconds   float64 `json:"used_gb_seconds" minimum:"0" doc:"The GB-seconds that the bot runners have used this month, from CloudWatch, read at most once per 10 minutes. 0 on a server that doesn't measure them, such as a local one."`
	BudgetGBSeconds float64 `json:"budget_gb_seconds" minimum:"0" doc:"The runners' monthly budget in GB-seconds (ISLAND_BATTLES_BOT_COMPUTE_BUDGET_GB_SECONDS)."`
	Available       bool    `json:"available" doc:"Whether a game with an uploaded bot, a member's or a bot seat's, may start: used_gb_seconds is under budget_gb_seconds. When false, the start is refused until the next month, and the bot pickers grey out uploaded bots. Games already running go on."`
}
