package main

import (
	"bytes"
	"context"
	"log/slog"
	"testing"

	"islandBattles/backend/dependencies"
	"islandBattles/backend/internal/testutils"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// Every deploy runs the command, so it must create the schema on an empty
// database and then find nothing left to do. The lobby's tables are not
// Island Battles'.
func TestMigrateCreatesTheSchemaAndRepeats(t *testing.T) {
	gameURL, _ := testutils.StartTestDatabases(t)
	for range 2 {
		var output bytes.Buffer
		if err := run(context.Background(), dependencies.DefaultDatabaseConfig(gameURL), slog.New(slog.NewTextHandler(&output, nil))); err != nil {
			t.Fatal(err)
		}
	}
	database, err := gorm.Open(postgres.Open(gameURL), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if pool, err := database.DB(); err == nil {
		t.Cleanup(func() { _ = pool.Close() })
	}
	for _, table := range []string{"map_template_schema_migrations", "map_templates", "games", "generations"} {
		if !database.Migrator().HasTable(table) {
			t.Fatalf("missing table %s", table)
		}
	}
	var lobbyTables int64
	if err := database.Raw(`SELECT count(*) FROM pg_tables WHERE schemaname = current_schema() AND tablename LIKE 'lobby\_%'`).Scan(&lobbyTables).Error; err != nil || lobbyTables != 0 {
		t.Fatalf("the lobby's tables in Island Battles' database: %d, %v", lobbyTables, err)
	}
}
