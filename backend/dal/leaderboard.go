package dal

import (
	"errors"
	"strings"
	"time"

	"islandBattles/backend/models"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	// ErrCompetitorNotFound refuses a competitor that no live row has.
	ErrCompetitorNotFound = errors.New("competitor not found")
	// ErrNotABotName refuses to remove a competitor that isn't a bot name:
	// an account's hand play or a house bot.
	ErrNotABotName = errors.New("the competitor isn't a bot name")
)

var (
	competitorAccountColumn = clause.Column{Table: models.Competitor{}.TableName(), Name: "account_id"}
	accountIDColumn         = clause.Column{Name: "account_id"}
	ratingColumn            = clause.Column{Name: "rating"}
	gamesColumn             = clause.Column{Name: "games"}
	endedAtColumn           = clause.Column{Name: "ended_at"}
)

// leaderboardOrder is the leaderboard's order: rating, highest first, then
// more rated games first, then the older competitor, and the ID, so a page
// never shows a competitor twice or skips one.
var leaderboardOrder = clause.OrderBy{Columns: []clause.OrderByColumn{
	{Column: ratingColumn, Desc: true},
	{Column: gamesColumn, Desc: true},
	{Column: createdAtColumn},
	{Column: clause.Column{Name: "id"}},
}}

// listedCompetitors selects the competitors that the leaderboard lists, of
// one kind, or of every kind when kind is empty: live, with at least one
// rated game, and a house bot or of an account that the game knows and
// that isn't hidden.
func listedCompetitors(database *gorm.DB, kind models.CompetitorKind) *gorm.DB {
	visibleAccounts := database.Session(&gorm.Session{NewDB: true}).Model(&models.IslandAccount{}).Select("account_id").
		Where(clause.Eq{Column: clause.Column{Name: "hidden"}, Value: false})
	// A struct filter drops an empty kind, which lists every kind.
	return database.Model(&models.Competitor{}).Where(&models.Competitor{Kind: kind}).
		Where(clause.Gt{Column: gamesColumn, Value: 0}).
		Where(clause.Or(
			clause.Eq{Column: competitorAccountColumn, Value: nil},
			clause.Expr{SQL: "? IN (?)", Vars: []any{competitorAccountColumn, visibleAccounts}},
		))
}

// LeaderboardPage returns up to limit listed competitors of a kind, or of
// every kind when kind is empty, in the leaderboard's order, after skipping
// offset of them.
func LeaderboardPage(database *gorm.DB, kind models.CompetitorKind, limit, offset int) ([]models.Competitor, error) {
	var competitors []models.Competitor
	err := listedCompetitors(database, kind).Order(leaderboardOrder).Limit(limit).Offset(offset).Find(&competitors).Error
	return competitors, err
}

// CountListedCompetitors counts the listed competitors of a kind, or of
// every kind when kind is empty.
func CountListedCompetitors(database *gorm.DB, kind models.CompetitorKind) (int, error) {
	var count int64
	err := listedCompetitors(database, kind).Count(&count).Error
	return int(count), err
}

// CountListedAbove counts the listed competitors of a kind, or of every kind
// when kind is empty, whose rating as shown (models.RoundRating) is more
// than shown: those ranked ahead of a competitor shown at that rating.
func CountListedAbove(database *gorm.DB, kind models.CompetitorKind, shown int) (int, error) {
	var count int64
	err := listedCompetitors(database, kind).Where(clause.Gte{Column: ratingColumn, Value: float64(shown) + 0.5}).Count(&count).Error
	return int(count), err
}

// FindListedCompetitor returns the competitor with this ID when the
// leaderboard lists it, and nil otherwise: a deleted competitor, one of a
// hidden account, one without a rated game, or an unknown one.
func FindListedCompetitor(database *gorm.DB, competitorID string) (*models.Competitor, error) {
	// A struct filter drops an empty ID, which would match any competitor.
	if competitorID == "" {
		return nil, errors.New("find listed competitor: no competitor ID")
	}
	var found []models.Competitor
	if err := listedCompetitors(database, "").Where(&models.Competitor{ID: competitorID}).Limit(1).Find(&found).Error; err != nil {
		return nil, err
	}
	if len(found) == 0 {
		return nil, nil
	}
	return &found[0], nil
}

// CompetitorHistory returns a competitor's latest limit rating history
// rows, newest first.
func CompetitorHistory(database *gorm.DB, competitorID string, limit int) ([]models.RatingEvent, error) {
	// A struct filter drops an empty ID, which would read every history.
	if competitorID == "" {
		return nil, errors.New("competitor history: no competitor ID")
	}
	var events []models.RatingEvent
	err := database.Where(&models.RatingEvent{CompetitorID: competitorID}).
		Order(clause.OrderBy{Columns: []clause.OrderByColumn{{Column: endedAtColumn, Desc: true}, {Column: clause.Column{Name: "id"}, Desc: true}}}).
		Limit(limit).Find(&events).Error
	return events, err
}

// CompetitorsByID returns the competitors with these IDs, soft-deleted ones
// too, in no particular order. No IDs read nothing.
func CompetitorsByID(database *gorm.DB, competitorIDs []string) ([]models.Competitor, error) {
	if len(competitorIDs) == 0 {
		return nil, nil
	}
	var competitors []models.Competitor
	err := database.Unscoped().Where(clause.IN{Column: clause.Column{Name: "id"}, Values: anyValues(competitorIDs)}).Find(&competitors).Error
	return competitors, err
}

// AccountsByID returns the accounts with these IDs that the game knows, by
// account ID. No accounts read nothing.
func AccountsByID(database *gorm.DB, accountIDs []string) (map[string]models.IslandAccount, error) {
	accounts := map[string]models.IslandAccount{}
	if len(accountIDs) == 0 {
		return accounts, nil
	}
	var found []models.IslandAccount
	if err := database.Where(clause.IN{Column: accountIDColumn, Values: anyValues(accountIDs)}).Find(&found).Error; err != nil {
		return nil, err
	}
	for _, account := range found {
		accounts[account.AccountID] = account
	}
	return accounts, nil
}

// RemoveBotName soft-deletes a live bot name, as its owner's
// DeleteBotName does, for the admin: the name is free at once, and the
// owner can register another. A competitor that no live row has is
// ErrCompetitorNotFound, and an account's hand play or a house bot
// ErrNotABotName.
func RemoveBotName(database *gorm.DB, competitorID string) error {
	// A struct filter drops an empty ID, which would match every bot name.
	if competitorID == "" {
		return errors.New("remove bot name: no competitor ID")
	}
	var found []models.Competitor
	if err := database.Where(&models.Competitor{ID: competitorID}).Limit(1).Find(&found).Error; err != nil {
		return err
	}
	if len(found) == 0 {
		return ErrCompetitorNotFound
	}
	if found[0].Kind != models.BotCompetitor {
		return ErrNotABotName
	}
	removed := database.Model(&models.Competitor{}).
		Where(&models.Competitor{ID: competitorID, Kind: models.BotCompetitor}).
		Updates(retired(time.Now()))
	if removed.Error != nil {
		return removed.Error
	}
	if removed.RowsAffected == 0 {
		// Its owner deleted it in between.
		return ErrCompetitorNotFound
	}
	return nil
}

// SetAccountHidden hides an account's competitors from the leaderboard, or
// shows them again. It reports false when the game doesn't know the
// account.
func SetAccountHidden(database *gorm.DB, accountID string, hidden bool) (bool, error) {
	// A struct filter drops an empty ID, which would match every account.
	if accountID == "" {
		return false, errors.New("set account hidden: no account ID")
	}
	updated := database.Model(&models.IslandAccount{}).Where(&models.IslandAccount{AccountID: accountID}).
		Updates(map[string]any{"hidden": hidden, "updated_at": time.Now()})
	return updated.RowsAffected > 0, updated.Error
}

// anyValues are the values of an IN clause.
func anyValues(values []string) []any {
	converted := make([]any, len(values))
	for index, value := range values {
		converted[index] = value
	}
	return converted
}

// registeredBotsOrder is the registered bots' order: rated bots first, by
// the leaderboard's order, then unrated ones by name, and the ID, so a page
// never shows a bot twice or skips one.
var registeredBotsOrder = clause.OrderBy{Columns: []clause.OrderByColumn{
	{Column: clause.Column{Raw: true, Name: "games > 0"}, Desc: true},
	{Column: ratingColumn, Desc: true},
	{Column: gamesColumn, Desc: true},
	{Column: clause.Column{Name: "name_key"}},
	{Column: clause.Column{Name: "id"}},
}}

// registeredBots selects the bot names that anyone may choose by their
// competitor ID, rated or not: live, with a URL, of an account that the
// game knows and that isn't hidden. A search that isn't empty keeps the bots
// whose name or owner's display name holds it, without regard to case.
func registeredBots(database *gorm.DB, search string) *gorm.DB {
	accounts := database.Session(&gorm.Session{NewDB: true}).Model(&models.IslandAccount{}).Select("account_id").
		Where(clause.Eq{Column: clause.Column{Name: "hidden"}, Value: false})
	query := database.Model(&models.Competitor{}).Where(&models.Competitor{Kind: models.BotCompetitor}).
		Where(clause.Neq{Column: clause.Column{Name: "url"}, Value: nil}).
		Where(clause.Expr{SQL: "? IN (?)", Vars: []any{competitorAccountColumn, accounts}})
	if search == "" {
		return query
	}
	pattern := "%" + likeEscaper.Replace(strings.ToLower(search)) + "%"
	owners := database.Session(&gorm.Session{NewDB: true}).Model(&models.IslandAccount{}).Select("account_id").
		Where(clause.Expr{SQL: "LOWER(display_name) LIKE ? ESCAPE '\\'", Vars: []any{pattern}})
	return query.Where(clause.Or(
		clause.Expr{SQL: "name_key LIKE ? ESCAPE '\\'", Vars: []any{pattern}},
		clause.Expr{SQL: "? IN (?)", Vars: []any{competitorAccountColumn, owners}},
	))
}

// likeEscaper escapes LIKE's wildcards and its escape character, so a
// search matches them as they are.
var likeEscaper = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)

// RegisteredBotsPage returns up to limit registered bots that match search,
// in their order, after skipping offset of them.
func RegisteredBotsPage(database *gorm.DB, search string, limit, offset int) ([]models.Competitor, error) {
	var competitors []models.Competitor
	err := registeredBots(database, search).Order(registeredBotsOrder).Limit(limit).Offset(offset).Find(&competitors).Error
	return competitors, err
}

// CountRegisteredBots counts the registered bots that match search.
func CountRegisteredBots(database *gorm.DB, search string) (int, error) {
	var count int64
	err := registeredBots(database, search).Count(&count).Error
	return int(count), err
}
