package dependencies

import (
	"context"
	"fmt"
	"islandBattles/backend/models"
	lobbypostgres "islandBattles/lobby/postgres"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// InitDB opens and initializes the current database schema. It returns errors
// to bootstrap instead of terminating the process from a reusable dependency.
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

func initializeSchema(ctx context.Context, db *gorm.DB) error {
	if err := lobbypostgres.InitializeSchema(ctx, db); err != nil {
		return fmt.Errorf("initialize lobby schema: %w", err)
	}
	err := db.WithContext(ctx).AutoMigrate(&models.Game{}, &models.Map{}, &models.Castle{}, &models.CastleTemplate{},
		&models.MapTemplate{},
		&models.Generation{}, &models.SoldierGroup{}, &models.Command{}, &models.TeamBindingRecord{}, &models.FrozenGamePlayer{}, &models.GenerationInput{}, &models.GenerationRequest{}, &models.GenerationNotification{})
	if err != nil {
		return fmt.Errorf("initialize game schema: %w", err)
	}
	return nil
}
