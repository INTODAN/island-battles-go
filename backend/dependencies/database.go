package dependencies

import (
	"context"
	"fmt"
	"islandBattles/backend/dal"
	"islandBattles/backend/models"
	lobbypostgres "islandBattles/lobby/postgres"
	"log/slog"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

// SlowQueryThreshold is how long a query may take before it is logged as slow.
const SlowQueryThreshold = 200 * time.Millisecond

// InitDB connects to the database with a bounded pool. It runs no DDL, so a
// Lambda cold start stays fast and concurrent starts don't race; the schema
// comes from MigrateSchema, which runs once per deploy. It returns errors to
// bootstrap instead of terminating the process from a reusable dependency.
//
// GORM logs failed and slow queries through logger, so they share the host's
// format, such as JSON on Lambda. Queries are logged with placeholders rather
// than their values, which may hold credentials.
func InitDB(config DatabaseConfig, logger *slog.Logger) (*gorm.DB, error) {
	queryLogger := gormlogger.NewSlogLogger(logger, gormlogger.Config{
		SlowThreshold:             SlowQueryThreshold,
		LogLevel:                  gormlogger.Warn,
		IgnoreRecordNotFoundError: true,
		ParameterizedQueries:      true,
	})
	db, err := gorm.Open(postgres.Open(config.URL), &gorm.Config{Logger: queryLogger})
	if err != nil {
		return nil, fmt.Errorf("connect database: %w", err)
	}
	sqlDatabase, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("connect database: %w", err)
	}
	sqlDatabase.SetMaxOpenConns(config.MaxOpenConnections)
	sqlDatabase.SetMaxIdleConns(config.MaxIdleConnections)
	sqlDatabase.SetConnMaxIdleTime(config.ConnectionMaxIdleTime)
	return db, nil
}

// MigrateSchema brings the database to the schema this build expects. The
// migrate command runs it once per deploy, before any host serves requests.
// It runs the lobby and map template migrations, which keep their rows, and
// then AutoMigrates the game tables, whose data may be reset. Game tables
// come last so that they may reference templates.
func MigrateSchema(ctx context.Context, db *gorm.DB) error {
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
// insert. Old replays are game data and are not kept. IF EXISTS lets every
// later run of the migrate command find nothing to do.
func dropGameReplayColumn(ctx context.Context, db *gorm.DB) error {
	if err := db.WithContext(ctx).Exec(`ALTER TABLE games DROP COLUMN IF EXISTS replay`).Error; err != nil {
		return fmt.Errorf("drop the games replay column: %w", err)
	}
	return nil
}
