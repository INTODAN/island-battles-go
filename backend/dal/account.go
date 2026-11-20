package dal

import (
	"errors"
	"time"

	"islandBattles/backend/models"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	// ErrBotNameTaken refuses a bot name that a live competitor of any
	// account has, without regard to case.
	ErrBotNameTaken = errors.New("bot name taken")
	// ErrBotNameLimit refuses a bot name beyond models.MaxBotNames.
	ErrBotNameLimit = errors.New("bot name limit reached")
)

var competitorOrder = []clause.OrderByColumn{{Column: createdAtColumn}, {Column: clause.Column{Name: "id"}}}

// SeeAccount stores an account the first time a player token names it, with
// its manual competitor, and stores its new display name when it changed.
// Run it in a transaction, so the two rows are stored together. Calling it
// again changes nothing: a name that didn't change isn't written. Its upsert
// locks the account's row until the transaction ends, as ON CONFLICT DO
// UPDATE does even when its WHERE skips the update, so the account's later
// changes in the transaction wait for each other.
func SeeAccount(transaction *gorm.DB, accountID, displayName string) error {
	account := models.IslandAccount{AccountID: accountID, DisplayName: displayName}
	if err := transaction.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "account_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"display_name", "updated_at"}),
		Where: clause.Where{Exprs: []clause.Expression{clause.Neq{
			Column: clause.Column{Table: models.IslandAccount{}.TableName(), Name: "display_name"},
			Value:  clause.Column{Table: "excluded", Name: "display_name"},
		}}},
	}).Create(&account).Error; err != nil {
		return err
	}
	// The unique index on a live manual competitor's account keeps one.
	hand := models.Competitor{ID: uuid.NewString(), Kind: models.ManualCompetitor, AccountID: &accountID, Rating: models.InitialRating}
	return transaction.Clauses(clause.OnConflict{DoNothing: true}).Create(&hand).Error
}

// EnsureAccount stores an account that the game doesn't know, with its
// manual competitor, as SeeAccount does, but never changes the display name
// of an account it knows. Run it in a transaction.
func EnsureAccount(transaction *gorm.DB, accountID, displayName string) error {
	if accountID == "" {
		return errors.New("ensure account: no account ID")
	}
	account := models.IslandAccount{AccountID: accountID, DisplayName: displayName}
	if err := transaction.Clauses(clause.OnConflict{DoNothing: true}).Create(&account).Error; err != nil {
		return err
	}
	// The unique index on a live manual competitor's account keeps one.
	hand := models.Competitor{ID: uuid.NewString(), Kind: models.ManualCompetitor, AccountID: &accountID, Rating: models.InitialRating}
	return transaction.Clauses(clause.OnConflict{DoNothing: true}).Create(&hand).Error
}

// AccountCompetitors returns an account's live competitors, its manual one
// and its bot names, oldest first.
func AccountCompetitors(database *gorm.DB, accountID string) ([]models.Competitor, error) {
	var competitors []models.Competitor
	err := database.Where(&models.Competitor{AccountID: &accountID}).Order(clause.OrderBy{Columns: competitorOrder}).Find(&competitors).Error
	return competitors, err
}

// FindManualCompetitor returns an account's live manual competitor, its hand
// play, or nil when the game doesn't know the account (SeeAccount creates it).
func FindManualCompetitor(database *gorm.DB, accountID string) (*models.Competitor, error) {
	// A struct filter drops an empty field, which would match any account's.
	if accountID == "" {
		return nil, errors.New("find manual competitor: no account ID")
	}
	var found []models.Competitor
	if err := database.Where(&models.Competitor{AccountID: &accountID, Kind: models.ManualCompetitor}).Limit(1).Find(&found).Error; err != nil {
		return nil, err
	}
	if len(found) == 0 {
		return nil, nil
	}
	return &found[0], nil
}

// LiveBotNames returns the live bot names of the accounts, in one query, in
// no particular order. No accounts read nothing.
func LiveBotNames(database *gorm.DB, accountIDs []string) ([]models.Competitor, error) {
	if len(accountIDs) == 0 {
		return nil, nil
	}
	accounts := make([]any, len(accountIDs))
	for index, accountID := range accountIDs {
		accounts[index] = accountID
	}
	var competitors []models.Competitor
	err := database.Where(&models.Competitor{Kind: models.BotCompetitor}).
		Where(clause.IN{Column: clause.Column{Name: "account_id"}, Values: accounts}).
		Find(&competitors).Error
	return competitors, err
}

// AddBotName registers a bot name for an account, whose row the transaction
// holds locked (SeeAccount), at models.InitialRating. name is already
// normalized (models.NormalizeBotName). An account with models.MaxBotNames
// live bot names gets ErrBotNameLimit, and a name that a live competitor of
// any account has, without regard to case, ErrBotNameTaken.
func AddBotName(transaction *gorm.DB, accountID, name string) (models.Competitor, error) {
	var live int64
	if err := transaction.Model(&models.Competitor{}).Where(&models.Competitor{AccountID: &accountID, Kind: models.BotCompetitor}).Count(&live).Error; err != nil {
		return models.Competitor{}, err
	}
	if live >= models.MaxBotNames {
		return models.Competitor{}, ErrBotNameLimit
	}
	key := models.BotNameKey(name)
	competitor := models.Competitor{ID: uuid.NewString(), Kind: models.BotCompetitor, AccountID: &accountID, Name: &name, NameKey: &key, Rating: models.InitialRating}
	// The unique index on a live competitor's name_key is the only one a new
	// bot name can conflict with.
	created := transaction.Clauses(clause.OnConflict{DoNothing: true}).Create(&competitor)
	if created.Error != nil {
		return models.Competitor{}, created.Error
	}
	if created.RowsAffected == 0 {
		return models.Competitor{}, ErrBotNameTaken
	}
	return competitor, nil
}

// retired soft-deletes competitors and frees their names at once.
func retired(now time.Time) map[string]any {
	return map[string]any{"deleted_at": now, "name": nil, "name_key": nil}
}

// DeleteBotName soft-deletes one of an account's live bot names, which
// frees the name at once; a game in play still finds the row. It reports
// false when the account has no such live bot name.
func DeleteBotName(database *gorm.DB, accountID, competitorID string) (bool, error) {
	// A struct filter drops empty fields, which would match every bot name.
	if accountID == "" || competitorID == "" {
		return false, errors.New("delete bot name: no account or competitor ID")
	}
	deleted := database.Model(&models.Competitor{}).
		Where(&models.Competitor{ID: competitorID, AccountID: &accountID, Kind: models.BotCompetitor}).
		Updates(retired(time.Now()))
	return deleted.RowsAffected > 0, deleted.Error
}

// DeleteAccount deletes an account's row and soft-deletes every competitor
// of it, which frees its bot names. Deleting an account the game doesn't know
// changes nothing.
func DeleteAccount(database *gorm.DB, accountID string) error {
	if accountID == "" {
		return errors.New("delete account: no account ID")
	}
	return database.Transaction(func(transaction *gorm.DB) error {
		// The account's row goes first, so a concurrent change to its
		// competitors, which locks it, waits.
		if err := transaction.Where(&models.IslandAccount{AccountID: accountID}).Delete(&models.IslandAccount{}).Error; err != nil {
			return err
		}
		return transaction.Model(&models.Competitor{}).Where(&models.Competitor{AccountID: &accountID}).Updates(retired(time.Now())).Error
	})
}

// FindOrCreateHouseCompetitor returns the house bot of a default bot level,
// creating it at models.InitialRating when it is first needed. The levels
// are a list in code (bots.Defaults), so no migration creates them.
func FindOrCreateHouseCompetitor(database *gorm.DB, level uint) (models.Competitor, error) {
	find := func() ([]models.Competitor, error) {
		var found []models.Competitor
		err := database.Where(&models.Competitor{Kind: models.HouseCompetitor, HouseLevel: &level}).Limit(1).Find(&found).Error
		return found, err
	}
	found, err := find()
	if err != nil || len(found) == 1 {
		return firstCompetitor(found, err)
	}
	// The unique index on a live house bot's level keeps one, when two
	// requests create it at once.
	house := models.Competitor{ID: uuid.NewString(), Kind: models.HouseCompetitor, HouseLevel: &level, Rating: models.InitialRating}
	if err := database.Clauses(clause.OnConflict{DoNothing: true}).Create(&house).Error; err != nil {
		return models.Competitor{}, err
	}
	return firstCompetitor(find())
}

func firstCompetitor(found []models.Competitor, err error) (models.Competitor, error) {
	if err != nil {
		return models.Competitor{}, err
	}
	if len(found) == 0 {
		return models.Competitor{}, errors.New("the house competitor was not stored")
	}
	return found[0], nil
}
