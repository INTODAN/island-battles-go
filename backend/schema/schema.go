// Package schema migrates the database of the backend's process: the
// lobby's tables, which the lobby in this process keeps, the map templates
// and the game tables. Like backend/app, it builds part of the in-process
// lobby, so it may use the lobby's Go packages until the lobby moves.
package schema

import (
	"context"
	"fmt"

	"islandBattles/backend/dal"
	"islandBattles/backend/dependencies"
	lobbypostgres "islandBattles/lobby/postgres"

	"gorm.io/gorm"
)

// Migrate brings the database to the schema this build expects. The migrate
// command runs it once per deploy, before any host serves requests. It runs
// the lobby and map template migrations, which keep their rows, and then
// AutoMigrates the game tables, whose data may be reset. Game tables come
// last so that they may reference templates.
func Migrate(ctx context.Context, db *gorm.DB) error {
	if err := lobbypostgres.Migrate(ctx, db); err != nil {
		return fmt.Errorf("migrate lobby schema: %w", err)
	}
	if err := MapTemplateMigrations().Apply(ctx, db); err != nil {
		return fmt.Errorf("migrate map template schema: %w", err)
	}
	return dependencies.MigrateGameSchema(ctx, db)
}

// MapTemplateMigrations is the versioned schema of the map templates,
// dal.MapTemplateMigrationFiles, run by the lobby's migration runner, which
// takes over a database created before versioning.
func MapTemplateMigrations() lobbypostgres.MigrationSet {
	return lobbypostgres.MigrationSet{HistoryTable: dal.MapTemplateHistoryTable, Files: dal.MapTemplateMigrationFiles(), Adopt: true}
}
