package dependencies

import (
	"context"
	"fmt"
	"islandBattles/backend/models"
	"log/slog"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	gormlogger "gorm.io/gorm/logger"
	gormtracing "gorm.io/plugin/opentelemetry/tracing"
)

// SlowQueryThreshold is how long a query may take before it is logged as slow.
const SlowQueryThreshold = 200 * time.Millisecond

// ConnectTimeout bounds InitDB's first connection, well under Lambda's
// 10-second limit on a cold start's init.
const ConnectTimeout = 5 * time.Second

// InitDB connects to the database with a bounded pool. It runs no DDL, so a
// Lambda cold start stays fast and concurrent starts don't race; the schema
// comes from schema.Migrate, which runs once per deploy. It returns errors to
// bootstrap instead of terminating the process from a reusable dependency.
//
// GORM logs failed and slow queries through logger, so they share the host's
// format, such as JSON on Lambda. Queries are logged with placeholders rather
// than their values, which may hold credentials. Failed queries are warnings:
// some fail as expected, such as a unique violation that answers 409, and a
// request that fails because of one logs its own error line.
//
// Each query gets a span when the request's trace is sampled, also without
// its values (backend/observability).
func InitDB(config DatabaseConfig, logger *slog.Logger) (*gorm.DB, error) {
	queryLogger := gormlogger.NewSlogLogger(slog.New(warningsAtMost{logger.Handler()}), gormlogger.Config{
		SlowThreshold:             SlowQueryThreshold,
		LogLevel:                  gormlogger.Warn,
		IgnoreRecordNotFoundError: true,
		ParameterizedQueries:      true,
	})
	// GORM's own ping has no timeout, which could hold a Lambda cold start
	// past Lambda's 10-second limit on init.
	db, err := gorm.Open(postgres.Open(config.URL), &gorm.Config{Logger: queryLogger, DisableAutomaticPing: true})
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
	ctx, cancel := context.WithTimeout(context.Background(), ConnectTimeout)
	defer cancel()
	if err := sqlDatabase.PingContext(ctx); err != nil {
		_ = sqlDatabase.Close()
		return nil, fmt.Errorf("connect database: %w", err)
	}
	if err := db.Use(gormtracing.NewPlugin(gormtracing.WithoutMetrics(), gormtracing.WithoutQueryVariables())); err != nil {
		_ = sqlDatabase.Close()
		return nil, fmt.Errorf("trace database queries: %w", err)
	}
	return db, nil
}

// InitLobbyDB connects to the lobby's own database, for the lobby in this
// process, as InitDB connects to Island Battles'. It returns no database
// while the lobby runs on its own (config.LobbyURL).
func InitLobbyDB(config Config, logger *slog.Logger) (*gorm.DB, error) {
	if config.LobbyURL != "" {
		return nil, nil
	}
	database, err := InitDB(config.LobbyDatabaseConfig(), logger)
	if err != nil {
		return nil, fmt.Errorf("the lobby's database: %w", err)
	}
	return database, nil
}

// warningsAtMost writes errors as warnings.
type warningsAtMost struct{ slog.Handler }

func (handler warningsAtMost) Enabled(ctx context.Context, level slog.Level) bool {
	return handler.Handler.Enabled(ctx, min(level, slog.LevelWarn))
}

func (handler warningsAtMost) Handle(ctx context.Context, record slog.Record) error {
	record.Level = min(record.Level, slog.LevelWarn)
	return handler.Handler.Handle(ctx, record)
}

func (handler warningsAtMost) WithAttrs(attrs []slog.Attr) slog.Handler {
	return warningsAtMost{handler.Handler.WithAttrs(attrs)}
}

func (handler warningsAtMost) WithGroup(name string) slog.Handler {
	return warningsAtMost{handler.Handler.WithGroup(name)}
}

// MigrateGameSchema AutoMigrates the game tables, whose data may be reset.
// schema.Migrate runs it after the map templates' migrations, so that game
// tables may reference templates.
func MigrateGameSchema(ctx context.Context, db *gorm.DB) error {
	// A lobby plays one game per round, so the unique index on a game's
	// lobby alone gives way to one on its lobby and round. AutoMigrate never
	// drops an index, so it goes here, before AutoMigrate creates the new
	// one. IF EXISTS lets every later run find nothing to do.
	if err := db.WithContext(ctx).Exec(`DROP INDEX IF EXISTS idx_games_lobby_id`).Error; err != nil {
		return fmt.Errorf("drop the games lobby index: %w", err)
	}
	// Frozen players name their bot's kind since hosted and uploaded bots;
	// before, a NULL bot level meant the localhost bot. AutoMigrate adds the
	// column with the localhost default, and the rows from before it gain
	// their kind once, below.
	migrator := db.WithContext(ctx).Migrator()
	backfillBotKinds := migrator.HasTable(&models.FrozenGamePlayer{}) && !migrator.HasColumn(&models.FrozenGamePlayer{}, "bot_kind")
	err := db.WithContext(ctx).AutoMigrate(&models.Game{}, &models.Map{}, &models.Castle{},
		&models.Generation{}, &models.SoldierGroup{}, &models.Command{}, &models.TeamBindingRecord{}, &models.FrozenGamePlayer{}, &models.GenerationInput{}, &models.GenerationRequest{}, &models.GenerationLog{}, &models.GamePlayerBotMemory{}, &models.BotPackage{}, &models.BotComputeUsage{})
	if err != nil {
		return fmt.Errorf("initialize game schema: %w", err)
	}
	if backfillBotKinds {
		if err := backfillFrozenBotKinds(ctx, db); err != nil {
			return err
		}
	}
	// Each person's bot is in their lobby member settings now, so the table
	// of bot choices, with its foreign key to the lobby's memberships, is
	// gone. Its choices are dropped: players choose again.
	if err := db.WithContext(ctx).Exec(`DROP TABLE IF EXISTS island_lobby_bot_choices`).Error; err != nil {
		return fmt.Errorf("drop island_lobby_bot_choices: %w", err)
	}
	// Generation events are pushed after their turn commits, so the table that
	// queued them is gone. It held only game data.
	if err := db.WithContext(ctx).Exec(`DROP TABLE IF EXISTS generation_notifications`).Error; err != nil {
		return fmt.Errorf("drop generation_notifications: %w", err)
	}
	return dropGameReplayColumn(ctx, db)
}

// backfillFrozenBotKinds gives the frozen players stored before bot kinds
// their kind: default where they have a default bot's level, and localhost,
// the column's default, otherwise.
func backfillFrozenBotKinds(ctx context.Context, db *gorm.DB) error {
	err := db.WithContext(ctx).Model(&models.FrozenGamePlayer{}).Where(clause.Neq{Column: clause.Column{Name: "bot_level"}, Value: nil}).
		Update("bot_kind", models.DefaultBot).Error
	if err != nil {
		return fmt.Errorf("backfill the frozen players' bot kinds: %w", err)
	}
	return nil
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
