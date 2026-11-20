// Package schema migrates Island Battles' database: the tables that are never
// reset (the map templates, the accounts and their competitors) and the game
// tables. The lobby runs on its own, with its own database
// (github.com/INTODAN/mpl). The kept tables' migrations run on the lobby's
// migration runner, so this package may use the lobby's package postgres.
package schema

import (
	"context"
	"fmt"

	lobbypostgres "github.com/INTODAN/mpl/postgres"
	"islandBattles/backend/dal"
	"islandBattles/backend/dependencies"

	"gorm.io/gorm"
)

// Migrate brings the database to the schema this build expects. The migrate
// command runs it once per deploy, before any host serves requests. It runs
// the kept tables' migrations, which keep their rows, and then AutoMigrates
// the game tables, whose data may be reset. Game tables come last so that
// they may reference templates.
func Migrate(ctx context.Context, db *gorm.DB) error {
	if err := KeptMigrations().Apply(ctx, db); err != nil {
		return fmt.Errorf("migrate the kept tables' schema: %w", err)
	}
	return dependencies.MigrateGameSchema(ctx, db)
}

// KeptMigrations is the versioned schema of the tables that are never reset,
// dal.KeptMigrationFiles, run by the lobby's migration runner, which takes
// over a database whose map templates were created before versioning.
func KeptMigrations() lobbypostgres.MigrationSet {
	return lobbypostgres.MigrationSet{HistoryTable: dal.KeptHistoryTable, Files: dal.KeptMigrationFiles(), Adopt: true}
}
