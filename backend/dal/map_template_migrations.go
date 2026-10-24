package dal

import (
	"context"
	"embed"
	"io/fs"

	"islandBattles/lobby/postgres"

	"gorm.io/gorm"
)

//go:embed migrations/*.sql
var mapTemplateMigrationFiles embed.FS

// MapTemplateMigrations returns the versioned schema of map_templates and
// castle_templates, which survive game data resets, recorded in
// map_template_schema_migrations. Change it by adding the next numbered file
// to backend/dal/migrations; never edit an applied one. Game tables stay on
// AutoMigrate.
func MapTemplateMigrations() postgres.MigrationSet {
	files, err := fs.Sub(mapTemplateMigrationFiles, "migrations")
	if err != nil {
		panic(err) // The directory name is a valid constant.
	}
	return postgres.MigrationSet{HistoryTable: "map_template_schema_migrations", Files: files, AdoptTable: "map_templates"}
}

// MigrateMapTemplates brings the map template tables to the latest version.
func MigrateMapTemplates(ctx context.Context, db *gorm.DB) error {
	return MapTemplateMigrations().Apply(ctx, db)
}
