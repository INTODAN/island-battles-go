package dal

import (
	"embed"
	"io/fs"
)

//go:embed migrations/*.sql
var mapTemplateMigrationFiles embed.FS

// MapTemplateHistoryTable records the applied map template migrations.
const MapTemplateHistoryTable = "map_template_schema_migrations"

// MapTemplateMigrationFiles returns the versioned schema of map_templates and
// castle_templates, which survive game data resets, recorded in
// MapTemplateHistoryTable. Change it by adding the next numbered file to
// backend/dal/migrations; never edit an applied one. Game tables stay on
// AutoMigrate. schema.MapTemplateMigrations applies them.
func MapTemplateMigrationFiles() fs.FS {
	files, err := fs.Sub(mapTemplateMigrationFiles, "migrations")
	if err != nil {
		panic(err) // The directory name is a valid constant.
	}
	return files
}
