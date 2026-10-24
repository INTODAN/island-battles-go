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
		&models.Generation{}, &models.SoldierGroup{}, &models.Command{}, &models.TeamBindingRecord{}, &models.FrozenGamePlayer{}, &models.GenerationInput{}, &models.GenerationRequest{})
	if err != nil {
		return fmt.Errorf("initialize game schema: %w", err)
	}
	// Generation events are pushed after their turn commits, so the table that
	// queued them is gone. It held only game data.
	if err := db.WithContext(ctx).Exec(`DROP TABLE IF EXISTS generation_notifications`).Error; err != nil {
		return fmt.Errorf("drop generation_notifications: %w", err)
	}
	return dropGameReplayColumn(ctx, db)
}

// dropGameReplayColumn removes the replay blob that games no longer store,
// because replays are derived from their generations. AutoMigrate never drops
// columns, and a leftover NOT NULL column would make every new game fail to
// insert. Old replays are game data and are not kept.
func dropGameReplayColumn(ctx context.Context, db *gorm.DB) error {
	migrator := db.WithContext(ctx).Migrator()
	if !migrator.HasColumn(&models.Game{}, "replay") {
		return nil
	}
	if err := migrator.DropColumn(&models.Game{}, "replay"); err != nil {
		return fmt.Errorf("drop the games replay column: %w", err)
	}
	return nil
}
