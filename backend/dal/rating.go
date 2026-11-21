package dal

import (
	"errors"
	"time"

	"islandBattles/backend/models"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// LockCompetitors returns the competitors with these IDs, soft-deleted ones
// too, and locks their rows until the transaction ends (SELECT … FOR
// UPDATE). It locks them in the order of their IDs, so two rated games that
// end at once, with competitors in common, wait for each other instead of
// deadlocking. No IDs lock nothing.
func LockCompetitors(transaction *gorm.DB, competitorIDs []string) ([]models.Competitor, error) {
	if len(competitorIDs) == 0 {
		return nil, nil
	}
	ids := make([]any, len(competitorIDs))
	for index, competitorID := range competitorIDs {
		ids[index] = competitorID
	}
	var competitors []models.Competitor
	err := transaction.Unscoped().
		Clauses(clause.Locking{Strength: clause.LockingStrengthUpdate}).
		Where(clause.IN{Column: clause.Column{Name: "id"}, Values: ids}).
		Order(clause.OrderByColumn{Column: clause.Column{Name: "id"}}).
		Find(&competitors).Error
	return competitors, err
}

// SaveCompetitorRecord stores a competitor's rating, its rated games, wins,
// draws and losses, and when it last played, soft-deleted or not.
func SaveCompetitorRecord(transaction *gorm.DB, competitor models.Competitor) error {
	// A model without its primary key would update every competitor.
	if competitor.ID == "" {
		return errors.New("save competitor record: no competitor ID")
	}
	return transaction.Unscoped().Model(&models.Competitor{ID: competitor.ID}).
		Select("Rating", "Games", "Wins", "Draws", "Losses", "LastPlayedAt").
		Updates(&competitor).Error
}

// StoreRatingEvents adds rating history rows. The unique index on a game and
// a competitor refuses a row of a competitor that the game already rated,
// which fails the transaction.
func StoreRatingEvents(transaction *gorm.DB, events []models.RatingEvent) error {
	if len(events) == 0 {
		return nil
	}
	return transaction.Create(&events).Error
}

// AccountDisplayNames returns the display names of the accounts that the
// game knows, by account ID. No accounts read nothing.
func AccountDisplayNames(database *gorm.DB, accountIDs []string) (map[string]string, error) {
	names := map[string]string{}
	if len(accountIDs) == 0 {
		return names, nil
	}
	accounts := make([]any, len(accountIDs))
	for index, accountID := range accountIDs {
		accounts[index] = accountID
	}
	var found []models.IslandAccount
	if err := database.Where(clause.IN{Column: clause.Column{Name: "account_id"}, Values: accounts}).Find(&found).Error; err != nil {
		return nil, err
	}
	for _, account := range found {
		names[account.AccountID] = account.DisplayName
	}
	return names, nil
}

// RatedPlayersSince returns the frozen players of rated games that started
// at since or later, whose competitor is one of competitorIDs, of every game,
// finished or not. Game retention keeps a game for a week, so a day's games
// are always there. No IDs read nothing.
func RatedPlayersSince(database *gorm.DB, competitorIDs []string, since time.Time) ([]models.FrozenGamePlayer, error) {
	if len(competitorIDs) == 0 {
		return nil, nil
	}
	var players []models.FrozenGamePlayer
	err := database.Where(clause.IN{Column: clause.Column{Name: "competitor_id"}, Values: anyValues(competitorIDs)}).
		Where(clause.Gte{Column: createdAtColumn, Value: since}).
		Find(&players).Error
	return players, err
}

// PauseAway pauses a bot name that missed a turn while it played away, with
// the reason its owner sees, soft-deleted or not. The transaction holds its
// row locked (LockCompetitors).
func PauseAway(transaction *gorm.DB, competitorID string, at time.Time, reason string) error {
	// A model without its primary key would update every competitor.
	if competitorID == "" {
		return errors.New("pause away: no competitor ID")
	}
	return transaction.Unscoped().Model(&models.Competitor{ID: competitorID}).
		Updates(map[string]any{"away_paused_at": at, "away_paused_reason": reason}).Error
}

// ResumeAway clears the away pause of one of an account's live bot names,
// paused or not, and returns the bot name. It returns nil when the account
// has no such live bot name.
func ResumeAway(database *gorm.DB, accountID, competitorID string) (*models.Competitor, error) {
	// A struct filter drops empty fields, which would match every bot name.
	if accountID == "" || competitorID == "" {
		return nil, errors.New("resume away: no account or competitor ID")
	}
	var changed []models.Competitor
	err := database.Model(&changed).Clauses(clause.Returning{}).
		Where(&models.Competitor{ID: competitorID, AccountID: &accountID, Kind: models.BotCompetitor}).
		Updates(map[string]any{"away_paused_at": nil, "away_paused_reason": nil}).Error
	if err != nil || len(changed) == 0 {
		return nil, err
	}
	return &changed[0], nil
}
