package models

import (
	"math"
	"time"
)

// Leaderboard paging: GET /leaderboard returns DefaultLeaderboardLimit
// entries unless it asks for 1 to MaxLeaderboardLimit, and a competitor's
// page its latest CompetitorHistoryLimit results.
const (
	DefaultLeaderboardLimit = 50
	MaxLeaderboardLimit     = 100
	CompetitorHistoryLimit  = 50
)

// The names a competitor's history shows for an opponent that was deleted
// since, or whose account is hidden: a bot name, or a person's hand play.
const (
	DeletedBotName    = "Deleted bot"
	DeletedPlayerName = "Deleted player"
)

// RoundRating is a rating as the leaderboard shows it: the nearest whole
// number, with halves rounded up, as a browser's Math.round does. A rating
// rounds to more than shown exactly when it is at least shown + 0.5, which
// is how the leaderboard counts the competitors ranked ahead of one.
func RoundRating(rating float64) int {
	whole := math.Floor(rating)
	if rating-whole >= 0.5 {
		whole++
	}
	return int(whole)
}

// LeaderboardEntryView is a competitor on the public leaderboard.
type LeaderboardEntryView struct {
	Rank         int            `json:"rank" minimum:"1" doc:"The place in the leaderboard's order, from 1: equal ratings, as shown, get the same rank, and the next rank skips as many places (1, 2, 2, 4). GET /leaderboard ranks within the kind it lists; a competitor's page among all kinds."`
	CompetitorID string         `json:"competitor_id" format:"uuid"`
	Kind         CompetitorKind `json:"kind" enum:"bot,manual,house" doc:"bot: a bot name that an account registered. manual: an account's hand play. house: a default bot level, which the backend plays."`
	Name         string         `json:"name" doc:"The name it is shown by: a bot's name, such as Kraken, the account's display name for its hand play, or Bot (level N) for a house bot."`
	Owner        *string        `json:"owner" nullable:"true" doc:"The display name of the account that a bot name belongs to, as in \"Kraken, by Dana\"; null for hand play and house bots."`
	Rating       int            `json:"rating" doc:"The Elo rating, rounded to a whole number."`
	Games        int            `json:"games" minimum:"1" doc:"Rated games played: a competitor is listed after its first."`
	Wins         int            `json:"wins" minimum:"0"`
	Draws        int            `json:"draws" minimum:"0"`
	Losses       int            `json:"losses" minimum:"0"`
	LastPlayedAt time.Time      `json:"last_played_at" doc:"When its latest rated game ended."`
}

// LeaderboardView is a page of the public leaderboard.
type LeaderboardView struct {
	Total   int                    `json:"total" minimum:"0" doc:"How many competitors the leaderboard lists, of the kind asked for."`
	Entries []LeaderboardEntryView `json:"entries" nullable:"false" doc:"The page's competitors, in the leaderboard's order: rating, highest first, then more rated games first, then the older competitor."`
}

// LeaderboardCompetitorView is one competitor's page: its entry, ranked
// among all kinds, and its latest rated results.
type LeaderboardCompetitorView struct {
	LeaderboardEntryView
	History []CompetitorResultView `json:"history" nullable:"false" doc:"The competitor's latest 50 rated results, newest first."`
}

// CompetitorResultView is one rated game of a competitor, from the rating
// history, which outlives the game.
type CompetitorResultView struct {
	EndedAt      time.Time                      `json:"ended_at" doc:"When the game ended."`
	Result       RatingResult                   `json:"result" enum:"win,draw,loss"`
	RatingBefore int                            `json:"rating_before" doc:"The competitor's rating before the game, rounded."`
	RatingAfter  int                            `json:"rating_after" doc:"The competitor's rating after the game, rounded."`
	Opponents    []CompetitorResultOpponentView `json:"opponents" nullable:"false" doc:"The game's other teams' competitors, in colour order."`
}

// CompetitorResultOpponentView is another team's competitor in a rated
// game, by the name it is shown by now.
type CompetitorResultOpponentView struct {
	CompetitorID *string `json:"competitor_id" format:"uuid" nullable:"true" doc:"Null for a competitor that was deleted since, or whose account is hidden."`
	Name         string  `json:"name" doc:"The name it is shown by now, as on the leaderboard; Deleted bot or Deleted player for a competitor that was deleted since, or whose account is hidden."`
	Owner        *string `json:"owner" nullable:"true" doc:"The display name of the account that a bot name belongs to; null otherwise, and for a deleted or hidden competitor."`
	RatingBefore int     `json:"rating_before" doc:"Its rating before the game, rounded."`
}

// SetAccountHiddenRequest hides an account's competitors from the
// leaderboard, or shows them again.
type SetAccountHiddenRequest struct {
	Hidden bool `json:"hidden" doc:"true hides the account's hand play and every bot name of it from the leaderboard and from other competitors' history; false shows them again."`
}
