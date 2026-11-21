package models

import (
	"errors"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"gorm.io/gorm"
)

// MaxBotNames is how many bot names one account may register.
const MaxBotNames = 5

// InitialRating is every competitor's rating before its first rated game.
const InitialRating = 1500.0

// Bot names are MinBotNameLength to MaxBotNameLength characters long after
// trimming.
const (
	MinBotNameLength = 3
	MaxBotNameLength = 24
)

// houseBotNamePrefix starts the names of the house bots, such as
// "Bot (level 2)", which no registered name may imitate.
const houseBotNamePrefix = "bot (level"

// ErrInvalidBotName rejects a bot name that breaks the rule of
// NormalizeBotName.
var ErrInvalidBotName = errors.New("a bot name is 3 to 24 characters of letters, digits, spaces, - and _, and can't start with \"Bot (level\"")

// NormalizeBotName returns name trimmed, or ErrInvalidBotName when it breaks
// the rule: 3 to 24 characters after trimming, of letters, digits, spaces,
// - and _, and not starting with "Bot (level", how the house bots are shown.
func NormalizeBotName(name string) (string, error) {
	if !utf8.ValidString(name) {
		return "", ErrInvalidBotName
	}
	name = strings.TrimSpace(name)
	if length := utf8.RuneCountInString(name); length < MinBotNameLength || length > MaxBotNameLength {
		return "", ErrInvalidBotName
	}
	for _, character := range name {
		if !unicode.IsLetter(character) && !unicode.IsDigit(character) && character != ' ' && character != '-' && character != '_' {
			return "", ErrInvalidBotName
		}
	}
	if strings.HasPrefix(strings.ToLower(name), houseBotNamePrefix) {
		return "", ErrInvalidBotName
	}
	return name, nil
}

// BotNameKey is the key that makes bot names unique across all accounts
// without regard to case: the name in lower case.
func BotNameKey(name string) string { return strings.ToLower(name) }

// IslandAccount is a lobby account as the game knows it: the lobby's account
// ID and the display name of the account's latest player token. The game
// learns it from player tokens, never from the lobby's tables. Accounts are
// never reset and outlive every game (backend/dal/migrations).
type IslandAccount struct {
	AccountID   string `gorm:"column:account_id;type:uuid;primaryKey"`
	DisplayName string `gorm:"column:display_name;not null"`
	// Hidden keeps the account's competitors off the leaderboard.
	Hidden    bool      `gorm:"column:hidden;not null;default:false"`
	CreatedAt time.Time `gorm:"column:created_at;not null"`
	UpdatedAt time.Time `gorm:"column:updated_at;not null"`
}

func (IslandAccount) TableName() string { return "island_accounts" }

// CompetitorKind says what a competitor is.
type CompetitorKind string

const (
	// BotCompetitor is a bot name that an account registered.
	BotCompetitor CompetitorKind = "bot"
	// ManualCompetitor is an account's hand play, one per account, shown
	// under the account's display name.
	ManualCompetitor CompetitorKind = "manual"
	// HouseCompetitor is a default bot level, shown as its bot seat is.
	HouseCompetitor CompetitorKind = "house"
)

// Competitor is what has a rating: a registered bot name, an account's hand
// play or a house bot. Competitors are never reset and outlive every game, so
// nothing ties them to a game. Deleting one is soft, so a game in play can
// still find it, and clears Name, NameKey and URL, which frees the name at
// once.
type Competitor struct {
	ID   string         `gorm:"column:id;type:uuid;primaryKey"`
	Kind CompetitorKind `gorm:"column:kind;not null"`
	// AccountID is the owner, nil for a house bot.
	AccountID *string `gorm:"column:account_id;type:uuid"`
	// Name is a bot's name; nil for the other kinds, which are shown by
	// their account's display name or their level.
	Name *string `gorm:"column:name"`
	// NameKey is Name in lower case, BotNameKey, unique among live
	// competitors.
	NameKey *string `gorm:"column:name_key"`
	// URL is a bot name's hosted bot URL (#400): where the name plays in a
	// rated lobby, and what the leaderboard shows once the name is listed.
	// Nil for the other kinds, and for a bot name registered before names
	// had URLs, until its owner adds one.
	URL *string `gorm:"column:url"`
	// HouseLevel is a house bot's default bot level.
	HouseLevel   *uint      `gorm:"column:house_level"`
	Rating       float64    `gorm:"column:rating;not null;default:1500"`
	Games        int        `gorm:"column:games;not null;default:0"`
	Wins         int        `gorm:"column:wins;not null;default:0"`
	Draws        int        `gorm:"column:draws;not null;default:0"`
	Losses       int        `gorm:"column:losses;not null;default:0"`
	LastPlayedAt *time.Time `gorm:"column:last_played_at"`
	// AwayPausedAt is when a bot name that played away, in a rated game of
	// someone else's lobby, missed a turn: it plays away in no rated game
	// until its owner resumes it. AwayPausedReason says which turn. Both nil
	// while it isn't paused.
	AwayPausedAt     *time.Time     `gorm:"column:away_paused_at"`
	AwayPausedReason *string        `gorm:"column:away_paused_reason"`
	CreatedAt        time.Time      `gorm:"column:created_at;not null"`
	DeletedAt        gorm.DeletedAt `gorm:"column:deleted_at"`
}

func (Competitor) TableName() string { return "island_competitors" }

// CompetitorView is a competitor with its rating and its rated games.
type CompetitorView struct {
	ID           string         `json:"id" format:"uuid"`
	Kind         CompetitorKind `json:"kind" enum:"bot,manual,house" doc:"bot: a bot name that an account registered. manual: an account's hand play, one per account. house: a default bot level."`
	Name         string         `json:"name" doc:"The name it is shown by: a bot's registered name, the account's display name for its hand play, or Bot (level N) for a house bot."`
	Rating       float64        `json:"rating" doc:"The Elo rating, 1500 before the first rated game. Shown rounded."`
	Games        int            `json:"games" minimum:"0" doc:"Rated games played."`
	Wins         int            `json:"wins" minimum:"0"`
	Draws        int            `json:"draws" minimum:"0"`
	Losses       int            `json:"losses" minimum:"0"`
	LastPlayedAt *time.Time     `json:"last_played_at" nullable:"true" doc:"When its latest rated game ended, or null before the first."`
	URL          *string        `json:"url" nullable:"true" doc:"For a bot name, its hosted bot's URL, where it plays in a rated lobby. It becomes public on the leaderboard once the name has played a rated game. Null for hand play, and for a bot name registered before names had URLs, which plays in no rated lobby until its owner sets one with PUT /account/bot-names/{competitor_id}."`
	AwayPaused   *AwayPauseView `json:"away_paused,omitempty" doc:"For a bot name that missed a turn while it played away, in a rated game of someone else's lobby: it plays away in no rated game until its owner resumes it with DELETE /account/bot-names/{competitor_id}/away-pause. Its owner's own games with it are unaffected. Absent otherwise."`
}

// AwayPauseView says why a bot name plays away in no rated game.
type AwayPauseView struct {
	At     time.Time `json:"at" doc:"When it missed the turn."`
	Reason string    `json:"reason" doc:"Which turn it missed, such as \"Missed turn 12 of a rated game on 21 November\"."`
}

// IslandAccountView is a signed-in player's account, as GET /account returns
// it.
type IslandAccountView struct {
	AccountID   string           `json:"account_id" format:"uuid" doc:"The lobby's account ID."`
	DisplayName string           `json:"display_name" doc:"The account's display name, from the player token. Renaming the account in the lobby reaches the game with the next player token."`
	MaxBotNames int              `json:"max_bot_names" doc:"How many bot names an account may register: 5."`
	Hand        CompetitorView   `json:"hand" doc:"The account's hand play, a manual competitor shown under the account's display name."`
	BotNames    []CompetitorView `json:"bot_names" nullable:"false" doc:"The account's bot names, oldest first."`
}

// RegisterBotNameRequest registers a bot name with its hosted bot's URL.
type RegisterBotNameRequest struct {
	Name string `json:"name" maxLength:"200" doc:"The bot's name: 3 to 24 characters after trimming, of letters, digits, spaces, - and _, not starting with Bot (level. It is unique across all accounts without regard to case."`
	URL  string `json:"url" maxLength:"2048" doc:"The bot's hosted bot URL, where the server sends it each turn in a rated lobby: https:// on port 443, without credentials, on a host name (not an IP address) whose addresses are all public, as a hosted bot's. It becomes public on the leaderboard once the name has played a rated game, so that others can play against it."`
}

// SetBotNameURLRequest changes a bot name's hosted bot URL.
type SetBotNameURLRequest struct {
	URL string `json:"url" maxLength:"2048" doc:"The bot's hosted bot URL, where the server sends it each turn in a rated lobby: https:// on port 443, without credentials, on a host name (not an IP address) whose addresses are all public, as a hosted bot's. It becomes public on the leaderboard once the name has played a rated game, so that others can play against it."`
}
