package main

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"islandBattles/backend/dependencies"
	"islandBattles/backend/internal/testutils"

	gormpostgres "gorm.io/driver/postgres"
)

// Every deploy runs the command, so it must create the schema on an empty
// database and then find nothing left to do.
func TestMigrateCreatesTheSchemaAndRepeats(t *testing.T) {
	database := testutils.InitEmptyTestDB(t)
	dialector, ok := database.Dialector.(*gormpostgres.Dialector)
	if !ok {
		t.Fatalf("unexpected dialector %T", database.Dialector)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	for range 2 {
		if err := run(context.Background(), dependencies.DefaultDatabaseConfig(dialector.DSN), logger); err != nil {
			t.Fatal(err)
		}
	}
	for _, table := range []string{"lobby_schema_migrations", "lobby_rooms", "map_template_schema_migrations", "map_templates", "games", "generations"} {
		if !database.Migrator().HasTable(table) {
			t.Fatalf("missing table %s", table)
		}
	}
}
