package main

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"islandBattles/backend/dependencies"
	"islandBattles/backend/internal/testutils"
	lobbypostgres "islandBattles/lobby/postgres"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func discard() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func lookup(values map[string]string) func(string) (string, bool) {
	return func(name string) (string, bool) {
		value, found := values[name]
		return value, found
	}
}

func open(t *testing.T, url string) *gorm.DB {
	t.Helper()
	database, err := gorm.Open(postgres.Open(url), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if pool, err := database.DB(); err == nil {
		t.Cleanup(func() { _ = pool.Close() })
	}
	return database
}

// lobbyTables lists the lobby's tables in database.
func lobbyTables(t *testing.T, database *gorm.DB) []string {
	t.Helper()
	var tables []string
	if err := database.Raw(`SELECT tablename FROM pg_tables WHERE schemaname = current_schema() AND tablename LIKE 'lobby\_%' ORDER BY tablename`).
		Scan(&tables).Error; err != nil {
		t.Fatal(err)
	}
	return tables
}

// migrate runs the command as a deploy does, with the environment given.
func migrate(t *testing.T, environment map[string]string) {
	t.Helper()
	config, err := dependencies.LoadDatabaseConfigFromLookup(lookup(environment))
	if err != nil {
		t.Fatal(err)
	}
	options, err := loadOptions(config, lookup(environment))
	if err != nil {
		t.Fatal(err)
	}
	if err := run(context.Background(), config, options, discard()); err != nil {
		t.Fatal(err)
	}
}

// Every deploy runs the command, so it must create the schema on an empty
// database and then find nothing left to do. The lobby's tables are not
// Island Battles'.
func TestMigrateCreatesTheSchemaAndRepeats(t *testing.T) {
	gameURL, lobbyURL := testutils.StartTestDatabases(t)
	for range 2 {
		migrate(t, map[string]string{dependencies.DatabaseURLEnv: gameURL, dependencies.LobbyDatabaseURLEnv: lobbyURL})
	}
	database := open(t, gameURL)
	for _, table := range []string{"map_template_schema_migrations", "map_templates", "games", "generations"} {
		if !database.Migrator().HasTable(table) {
			t.Fatalf("missing table %s", table)
		}
	}
	if tables := lobbyTables(t, database); len(tables) != 0 {
		t.Fatalf("the lobby's tables in Island Battles' database: %v", tables)
	}
}

// The switch to the lobby's own database (#229): a database that a build
// before it migrated holds the lobby's tables. Both migrate commands run, each
// on its own database, and the drop leaves no lobby table in Island Battles'
// database, and Island Battles' tables and the lobby's in theirs.
func TestTheSwitchDropsTheLobbysTablesFromTheGameDatabase(t *testing.T) {
	gameURL, lobbyURL := testutils.StartTestDatabases(t)
	game, lobby := open(t, gameURL), open(t, lobbyURL)
	// What builds before the switch left: the lobby's tables beside the
	// game's, in one database, with a lobby in them.
	if err := lobbypostgres.Migrate(context.Background(), game); err != nil {
		t.Fatal(err)
	}
	migrate(t, map[string]string{dependencies.DatabaseURLEnv: gameURL})
	if err := game.Exec(`INSERT INTO lobby_players (id, display_name, created_at) VALUES (gen_random_uuid(), 'Player', now())`).Error; err != nil {
		t.Fatal(err)
	}

	// The deploy: the lobby's migrations on the lobby's database, then
	// Island Battles' with the lobby's database configured.
	if err := lobbypostgres.Migrate(context.Background(), lobby); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		migrate(t, map[string]string{dependencies.DatabaseURLEnv: gameURL, dependencies.LobbyDatabaseURLEnv: lobbyURL})
	}

	if tables := lobbyTables(t, game); len(tables) != 0 {
		t.Fatalf("the lobby's tables left in Island Battles' database: %v", tables)
	}
	for _, table := range []string{"map_templates", "games", "generations", "frozen_game_players"} {
		if !game.Migrator().HasTable(table) {
			t.Fatalf("Island Battles' table %s is gone", table)
		}
	}
	tables := lobbyTables(t, lobby)
	for _, table := range []string{"lobby_schema_migrations", "lobby_rooms", "lobby_sessions"} {
		if !lobby.Migrator().HasTable(table) {
			t.Fatalf("the lobby's database lacks %s: %v", table, tables)
		}
	}
	var others []string
	if err := lobby.Raw(`SELECT tablename FROM pg_tables WHERE schemaname = current_schema() AND tablename NOT LIKE 'lobby\_%'`).Scan(&others).Error; err != nil {
		t.Fatal(err)
	}
	if len(others) != 0 {
		t.Fatalf("Island Battles' tables in the lobby's database: %v", others)
	}
}

// Without a separate lobby database, the lobby's tables in Island Battles'
// database are the only ones it has, so they stay: when LOBBY_DATABASE_URL
// is unset, and when it names Island Battles' database itself.
func TestTheDropIsSkippedWithoutASeparateLobbyDatabase(t *testing.T) {
	gameURL, _ := testutils.StartTestDatabases(t)
	game := open(t, gameURL)
	if err := lobbypostgres.Migrate(context.Background(), game); err != nil {
		t.Fatal(err)
	}
	before := lobbyTables(t, game)
	if len(before) == 0 {
		t.Fatal("no lobby tables to keep")
	}
	for _, environment := range []map[string]string{
		{dependencies.DatabaseURLEnv: gameURL},
		{dependencies.DatabaseURLEnv: gameURL, dependencies.LobbyDatabaseURLEnv: ""},
		{dependencies.DatabaseURLEnv: gameURL, dependencies.LobbyDatabaseURLEnv: gameURL},
	} {
		migrate(t, environment)
		if after := lobbyTables(t, game); len(after) != len(before) {
			t.Fatalf("with %v, the lobby's tables went from %v to %v", environment, before, after)
		}
	}
}

func TestLoadOptionsComparesTheDatabases(t *testing.T) {
	const game = "postgres://island_battles:secret@vm.example.org:5432/island_battles?sslmode=require"
	for _, test := range []struct {
		lobby    string
		separate bool
	}{
		{"", false},
		{game, false},
		{"postgres://lobby:other@vm.example.org:5432/island_battles?sslmode=require", false},
		{"postgres://lobby:secret@vm.example.org:5432/lobby?sslmode=require", true},
		{"postgres://lobby:secret@vm.example.org/lobby?sslmode=require", true},
		{"postgres://island_battles:secret@other.example.org:5432/island_battles?sslmode=require", true},
		{"postgres://island_battles:secret@vm.example.org:5433/island_battles?sslmode=require", true},
	} {
		options, err := loadOptions(dependencies.DefaultDatabaseConfig(game), lookup(map[string]string{dependencies.LobbyDatabaseURLEnv: test.lobby}))
		if err != nil {
			t.Fatal(err)
		}
		if options.SeparateLobbyDatabase != test.separate {
			t.Errorf("%q: separate = %v, want %v", test.lobby, options.SeparateLobbyDatabase, test.separate)
		}
	}
	if _, err := loadOptions(dependencies.DefaultDatabaseConfig(game), lookup(map[string]string{dependencies.LobbyDatabaseURLEnv: "postgres://%zz"})); err == nil {
		t.Fatal("accepted an invalid lobby database URL")
	}
}

func TestLocalhostIsOneHost(t *testing.T) {
	options, err := loadOptions(dependencies.DefaultDatabaseConfig("postgres://a:b@localhost:5432/islandBattles"),
		lookup(map[string]string{dependencies.LobbyDatabaseURLEnv: "postgres://a:b@127.0.0.1:5432/islandBattles"}))
	if err != nil {
		t.Fatal(err)
	}
	if options.SeparateLobbyDatabase {
		t.Fatal("localhost and 127.0.0.1 counted as two servers")
	}
}
