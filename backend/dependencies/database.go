package dependencies

import (
	"context"
	"fmt"
	"islandBattles/backend/dal"
	"islandBattles/backend/models"
	lobbypostgres "islandBattles/lobby/postgres"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// InitDB opens and migrates the database schema. It returns errors to
// bootstrap instead of terminating the process from a reusable dependency.
func InitDB(connectionString string) (*gorm.DB, error) {
	db, err := gorm.Open(postgres.Open(connectionString), &gorm.Config{})
	if err != nil {
		return nil, fmt.Errorf("connect database: %w", err)
	}
	if err := initializeSchema(context.Background(), db); err != nil {
		if sqlDatabase, closeError := db.DB(); closeError == nil {
			_ = sqlDatabase.Close()
		}
		return nil, err
	}
	return db, nil
}

// initializeSchema runs the lobby and map template migrations, which keep
// their rows, and then AutoMigrates the game tables, whose data may be reset.
// Game tables come last so that they may reference templates.
func initializeSchema(ctx context.Context, db *gorm.DB) error {
	if err := lobbypostgres.Migrate(ctx, db); err != nil {
		return fmt.Errorf("migrate lobby schema: %w", err)
	}
	if err := dal.MigrateMapTemplates(ctx, db); err != nil {
		return fmt.Errorf("migrate map template schema: %w", err)
	}
	err := db.WithContext(ctx).AutoMigrate(&models.Game{}, &models.Map{}, &models.Castle{},
		&models.Generation{}, &models.SoldierGroup{}, &models.Command{}, &models.TeamBindingRecord{}, &models.FrozenGamePlayer{}, &models.GenerationInput{}, &models.GenerationRequest{}, &models.GenerationNotification{})
	if err != nil {
		return fmt.Errorf("initialize game schema: %w", err)
	}
	return nil
}
