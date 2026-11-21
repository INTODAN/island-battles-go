package models

import "time"

// RatingResult is how a rated game ended for one competitor.
type RatingResult string

const (
	RatingWin  RatingResult = "win"
	RatingDraw RatingResult = "draw"
	RatingLoss RatingResult = "loss"
)

// RatingEvent is one competitor's rating change from one rated game, written
// in the transaction that stores the game's outcome. The history is never
// reset and outlives the game: GameID has no foreign key, and game retention
// never deletes it (backend/dal/migrations). The unique pair of GameID and
// CompetitorID stops a game from being rated twice.
type RatingEvent struct {
	ID           uint64       `gorm:"column:id;primaryKey"`
	GameID       uint         `gorm:"column:game_id;not null"`
	CompetitorID string       `gorm:"column:competitor_id;type:uuid;not null"`
	Result       RatingResult `gorm:"column:result;not null"`
	RatingBefore float64      `gorm:"column:rating_before;not null"`
	RatingAfter  float64      `gorm:"column:rating_after;not null"`
	// K is the competitor's K in this game: 40 for its first 30 rated
	// games, then 20 (rating.K).
	K int `gorm:"column:k;not null"`
	// Opponents are the game's other teams' competitors, without names,
	// which are read from the competitors when shown.
	Opponents []RatingOpponent `gorm:"column:opponents;serializer:json;type:jsonb;not null"`
	// Away is set when the competitor played away: a bot name in a seat, or
	// chosen by a member of another account.
	Away    bool      `gorm:"column:away;not null;default:false"`
	EndedAt time.Time `gorm:"column:ended_at;not null"`
}

func (RatingEvent) TableName() string { return "island_rating_events" }

// RatingOpponent is another team's competitor in a rated game, with its
// rating before the game.
type RatingOpponent struct {
	CompetitorID string  `json:"competitor_id"`
	RatingBefore float64 `json:"rating_before"`
}

// RatingChange is how a rated game changed one team's competitor's rating,
// as the game's outcome stores and shows it.
type RatingChange struct {
	TeamColor    uint           `json:"team_color" minimum:"1"`
	CompetitorID string         `json:"competitor_id" format:"uuid"`
	Kind         CompetitorKind `json:"kind" enum:"bot,manual,house" doc:"bot: a bot name that an account registered. manual: an account's hand play. house: a default bot level."`
	Name         string         `json:"name" doc:"The name the competitor is shown by: a bot's name, such as Kraken, the account's display name for its hand play, or Bot (level N) for a house bot."`
	Owner        string         `json:"owner,omitempty" doc:"The display name of the account that the bot name belongs to, for kind bot only."`
	Before       float64        `json:"before" doc:"The competitor's rating before the game, a decimal: show it rounded."`
	After        float64        `json:"after" doc:"The competitor's rating after the game, a decimal: show it rounded."`
}
