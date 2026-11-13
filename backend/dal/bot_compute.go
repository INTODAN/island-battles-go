package dal

import (
	"time"

	"islandBattles/backend/models"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// botComputeLock is the transaction-level advisory lock that serializes the
// refreshes of the bot runners' cached compute, so that concurrent requests
// read CloudWatch once between them.
const botComputeLock = 226_000_001

// LockBotComputeUsage takes the bot compute cache's advisory lock until the
// end of transaction.
func LockBotComputeUsage(transaction *gorm.DB) error {
	return transaction.Exec("SELECT pg_advisory_xact_lock(?)", botComputeLock).Error
}

// FindBotComputeUsage returns the cached compute of month, or nil when the
// cache holds none for it.
func FindBotComputeUsage(database *gorm.DB, month time.Time) (*models.BotComputeUsage, error) {
	var rows []models.BotComputeUsage
	if err := database.Where(&models.BotComputeUsage{Month: month}).Limit(1).Find(&rows).Error; err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return &rows[0], nil
}

// SaveBotComputeUsage keeps usage as the cache's one row, replacing the row
// of an earlier month.
func SaveBotComputeUsage(database *gorm.DB, usage models.BotComputeUsage) error {
	if err := database.Where(clause.Neq{Column: clause.Column{Name: "month"}, Value: usage.Month}).Delete(&models.BotComputeUsage{}).Error; err != nil {
		return err
	}
	return database.Clauses(clause.OnConflict{UpdateAll: true}).Create(&usage).Error
}
