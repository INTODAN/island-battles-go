package dal

import (
	"embed"
	"io/fs"
)

//go:embed migrations/*.sql
var keptMigrationFiles embed.FS

// KeptHistoryTable records the applied migrations of the kept tables. It
// keeps the name it had when only the map templates had migrations.
const KeptHistoryTable = "map_template_schema_migrations"

// KeptMigrationFiles returns the versioned schema of the tables that are
// never reset, recorded in KeptHistoryTable: the map templates
// (map_templates, castle_templates), and the accounts, their competitors and
// the rating history (island_accounts, island_competitors,
// island_rating_events), which hold what players earned.
// Change it by adding the next numbered file to backend/dal/migrations; never
// edit an applied one. Game tables stay on AutoMigrate.
// schema.KeptMigrations applies them.
func KeptMigrationFiles() fs.FS {
	files, err := fs.Sub(keptMigrationFiles, "migrations")
	if err != nil {
		panic(err) // The directory name is a valid constant.
	}
	return files
}
