package lobbyadapter

import (
	"context"
	"errors"

	"gorm.io/gorm"
)

// InitializeSchema creates only the Island Battles-specific team mapping.
// Generic lobby and game tables are initialized separately by bootstrap.
func InitializeSchema(ctx context.Context, database *gorm.DB) error {
	if database == nil {
		return errors.New("lobby adapter: nil database")
	}
	return database.WithContext(ctx).Transaction(func(transaction *gorm.DB) error {
		return transaction.AutoMigrate(&TeamBindingRecord{})
	})
}
