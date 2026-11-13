package main

import (
	"bytes"
	"context"
	"log/slog"
	"net/url"
	"strings"
	"testing"

	lobbypostgres "github.com/INTODAN/mpl/postgres"
	"islandBattles/backend/dependencies"
	"islandBattles/backend/internal/testutils"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func open(t *testing.T, databaseURL string) *gorm.DB {
	t.Helper()
	database, err := gorm.Open(postgres.Open(databaseURL), &gorm.Config{})
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

// migrate runs the command as a deploy does, with Island Battles' database
// and the lobby's URL, and returns what it logged.
func migrate(t *testing.T, gameURL, lobbyURL string) string {
	t.Helper()
	var output bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&output, nil))
	if err := run(context.Background(), dependencies.DefaultDatabaseConfig(gameURL), lobbyURL, logger); err != nil {
		t.Fatal(err)
	}
	return output.String()
}

// migrateLobby runs the lobby's migrations, as github.com/INTODAN/mpl/cmd/migrate does.
func migrateLobby(t *testing.T, database *gorm.DB) {
	t.Helper()
	if err := lobbypostgres.Migrate(context.Background(), database); err != nil {
		t.Fatal(err)
	}
}

// withHost returns databaseURL with another host, and a query parameter more,
// so it reads differently.
func withHost(t *testing.T, databaseURL, host string) string {
	t.Helper()
	parsed, err := url.Parse(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	parsed.Host = host + ":" + parsed.Port()
	query := parsed.Query()
	query.Set("application_name", "migrate-test")
	parsed.RawQuery = query.Encode()
	return parsed.String()
}

// Every deploy runs the command, so it must create the schema on an empty
// database and then find nothing left to do. The lobby's tables are not
// Island Battles'.
func TestMigrateCreatesTheSchemaAndRepeats(t *testing.T) {
	gameURL, lobbyURL := testutils.StartTestDatabases(t)
	migrateLobby(t, open(t, lobbyURL))
	for range 2 {
		migrate(t, gameURL, lobbyURL)
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

// oldGameDatabase gives Island Battles' database what builds before the
// switch left: the lobby's tables beside the game's, with a player in them.
func oldGameDatabase(t *testing.T, gameURL string) *gorm.DB {
	t.Helper()
	game := open(t, gameURL)
	migrateLobby(t, game)
	migrate(t, gameURL, "")
	if err := game.Exec(`INSERT INTO lobby_players (id, display_name, created_at) VALUES (gen_random_uuid(), 'Player', now())`).Error; err != nil {
		t.Fatal(err)
	}
	if len(lobbyTables(t, game)) == 0 {
		t.Fatal("no lobby tables to keep or drop")
	}
	return game
}

// The switch to the lobby's own database (#229): a database that a build
// before it migrated holds the lobby's tables. Both migrate commands run, each
// on its own database, and the drop leaves no lobby table in Island Battles'
// database, and Island Battles' tables and the lobby's in theirs.
func TestTheSwitchDropsTheLobbysTablesFromTheGameDatabase(t *testing.T) {
	gameURL, lobbyURL := testutils.StartTestDatabases(t)
	game, lobby := oldGameDatabase(t, gameURL), open(t, lobbyURL)

	// The deploy: the lobby's migrations on the lobby's database, then
	// Island Battles' with the lobby's database configured.
	migrateLobby(t, lobby)
	for range 2 {
		migrate(t, gameURL, lobbyURL)
	}

	if tables := lobbyTables(t, game); len(tables) != 0 {
		t.Fatalf("the lobby's tables left in Island Battles' database: %v", tables)
	}
	for _, table := range []string{"map_templates", "games", "generations", "frozen_game_players"} {
		if !game.Migrator().HasTable(table) {
			t.Fatalf("Island Battles' table %s is gone", table)
		}
	}
	for _, table := range []string{"lobby_schema_migrations", "lobby_rooms", "lobby_sessions"} {
		if !lobby.Migrator().HasTable(table) {
			t.Fatalf("the lobby's database lacks %s", table)
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
// is unset, when it is Island Battles' URL, and when it reaches Island
// Battles' database through a URL that reads differently.
func TestTheDropIsSkippedWithoutASeparateLobbyDatabase(t *testing.T) {
	gameURL, _ := testutils.StartTestDatabases(t)
	game := oldGameDatabase(t, gameURL)
	before := lobbyTables(t, game)
	parsed, err := url.Parse(gameURL)
	if err != nil {
		t.Fatal(err)
	}
	otherHost := "127.0.0.1"
	if parsed.Hostname() == otherHost {
		otherHost = "localhost"
	}
	for _, lobbyURL := range []string{"", gameURL, withHost(t, gameURL, otherHost)} {
		output := migrate(t, gameURL, lobbyURL)
		if after := lobbyTables(t, game); len(after) != len(before) {
			t.Fatalf("with %q, the lobby's tables went from %v to %v", lobbyURL, before, after)
		}
		if lobbyURL != "" && !strings.Contains(output, "can't be told apart from Island Battles' database") {
			t.Errorf("with %q, the log doesn't say why the tables stay: %s", lobbyURL, output)
		}
	}
}

// A lobby migrate command that failed or didn't run is never followed by the
// drop: the lobby's database must have every lobby migration first.
func TestTheDropWaitsForTheLobbysMigrations(t *testing.T) {
	gameURL, lobbyURL := testutils.StartTestDatabases(t)
	game, lobby := oldGameDatabase(t, gameURL), open(t, lobbyURL)
	before := lobbyTables(t, game)

	output := migrate(t, gameURL, lobbyURL)
	if after := lobbyTables(t, game); len(after) != len(before) {
		t.Fatalf("without the lobby's migrations, the lobby's tables went from %v to %v", before, after)
	}
	if !strings.Contains(output, "lacks this build's lobby migrations") || !strings.Contains(output, "level=WARN") {
		t.Errorf("the log doesn't warn why the tables stay: %s", output)
	}

	// Only part of them, such as a lobby migrate command of an older build.
	migrateLobby(t, lobby)
	if err := lobby.Exec(`DELETE FROM lobby_schema_migrations WHERE version = (SELECT max(version) FROM lobby_schema_migrations)`).Error; err != nil {
		t.Fatal(err)
	}
	migrate(t, gameURL, lobbyURL)
	if after := lobbyTables(t, game); len(after) != len(before) {
		t.Fatalf("with part of the lobby's migrations, the lobby's tables went from %v to %v", before, after)
	}
}

// Two servers whose databases have the same name are told apart by their
// clusters' system identifiers, which a role that is no superuser reads too.
func TestTheSameNameOnAnotherServerIsAnotherDatabase(t *testing.T) {
	gameURL, _ := testutils.StartTestDatabases(t)
	game := oldGameDatabase(t, gameURL)
	otherServerURL, _ := testutils.StartTestDatabases(t)
	otherServer := open(t, otherServerURL)
	migrateLobby(t, otherServer)
	// The lobby connects as a role of its own, as in production.
	for _, statement := range []string{
		`CREATE ROLE lobby_reader LOGIN NOSUPERUSER PASSWORD 'lobby_reader'`,
		`GRANT SELECT ON lobby_schema_migrations TO lobby_reader`,
	} {
		if err := otherServer.Exec(statement).Error; err != nil {
			t.Fatal(err)
		}
	}
	parsed, err := url.Parse(otherServerURL)
	if err != nil {
		t.Fatal(err)
	}
	parsed.User = url.UserPassword("lobby_reader", "lobby_reader")
	if gameName, otherName := strings.TrimPrefix(mustParse(t, gameURL).Path, "/"), strings.TrimPrefix(parsed.Path, "/"); gameName != otherName {
		t.Fatalf("the test needs one name on both servers: %s and %s", gameName, otherName)
	}

	output := migrate(t, gameURL, parsed.String())
	if tables := lobbyTables(t, game); len(tables) != 0 {
		t.Fatalf("the lobby's tables stayed: %v; %s", tables, output)
	}
}

func mustParse(t *testing.T, databaseURL string) *url.URL {
	t.Helper()
	parsed, err := url.Parse(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	return parsed
}
