package main

import (
	"context"
	"islandBattles/backend/schema"
	"net/url"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	lobbypostgres "islandBattles/lobby/postgres"

	"gorm.io/gorm"
)

// grantAdminRole runs the Ansible SQL for the production read-only role, as
// the VM deploy does: the role, then each database's grants, each in one
// transaction. It returns the role's URLs.
func grantAdminRole(t *testing.T, owners testDatabases) databaseURLs {
	t.Helper()
	ownerURLs := urlsOf(t, owners)
	gameURL, err := url.Parse(ownerURLs.game)
	if err != nil {
		t.Fatal(err)
	}
	lobbyURL, err := url.Parse(ownerURLs.lobby)
	if err != nil {
		t.Fatal(err)
	}
	variables := map[string]string{
		"postgres_admin_role": "island_battles_admin", "postgres_admin_password": "adminPassword123",
		"postgres_app_role": gameURL.User.Username(), "postgres_database": strings.TrimPrefix(gameURL.Path, "/"),
		"lobby_app_role": lobbyURL.User.Username(), "lobby_database": strings.TrimPrefix(lobbyURL.Path, "/"),
	}
	for _, step := range []struct {
		template string
		database *gorm.DB
	}{
		{"admin-role.sql.j2", owners.game},
		{"admin-grants-game.sql.j2", owners.game},
		{"admin-grants-lobby.sql.j2", owners.lobby},
	} {
		script := adminSQL(t, step.template, variables)
		if err := step.database.Transaction(func(transaction *gorm.DB) error { return transaction.Exec(script).Error }); err != nil {
			t.Fatalf("%s: %v", step.template, err)
		}
	}
	asAdmin := func(owner *url.URL) string {
		admin := *owner
		admin.User = url.UserPassword("island_battles_admin", "adminPassword123")
		return admin.String()
	}
	return databaseURLs{game: asAdmin(gameURL), lobby: asAdmin(lobbyURL)}
}

func ownerValue(t *testing.T, owner *gorm.DB, statement string) string {
	t.Helper()
	var value string
	if err := owner.Raw(statement).Row().Scan(&value); err != nil {
		t.Fatalf("%s: %v", statement, err)
	}
	return value
}

// A session-level advisory lock taken through query outlives the rolled back
// transaction, and the server keeps its one connection to each database, so
// it must be released after each call. Otherwise it blocks the migrations,
// Island Battles' and the lobby's.
func TestAdvisoryLocksEndWithTheCall(t *testing.T) {
	owner := initDatabases(t)
	seed(t, owner)
	client := connect(t, openBothAs(t, grantAdminRole(t, owner)))
	call[QueryResult](t, client, "query", map[string]any{"database": GameDatabase, "sql": "SELECT pg_advisory_lock(hashtextextended('map_template_schema_migrations', 0)), pg_advisory_lock_shared(42)"})
	call[QueryResult](t, client, "query", map[string]any{"database": LobbyDatabase, "sql": "SELECT pg_advisory_lock(hashtextextended('lobby_schema_migrations', 0)), pg_advisory_lock_shared(42)"})
	call[QueryResult](t, client, "query", map[string]any{"database": GameDatabase, "sql": "SELECT 1"})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := schema.Migrate(ctx, owner.game.WithContext(ctx), schema.Options{SeparateLobbyDatabase: true}); err != nil {
		t.Fatalf("the migrations waited for the admin server's lock: %v", err)
	}
	if err := lobbypostgres.Migrate(ctx, owner.lobby.WithContext(ctx)); err != nil {
		t.Fatalf("the lobby's migrations waited for the admin server's lock: %v", err)
	}
	for _, database := range []*gorm.DB{owner.game, owner.lobby} {
		if locked := ownerValue(t, database, "SELECT pg_try_advisory_lock(42)::text"); locked != "true" {
			t.Errorf("another session can't take lock 42 after the call")
		}
		if held := ownerValue(t, database, "SELECT count(*)::text FROM pg_locks WHERE locktype = 'advisory' AND pid <> pg_backend_pid() AND database = (SELECT oid FROM pg_database WHERE datname = current_database())"); held != "0" {
			t.Errorf("%s advisory locks are still held", held)
		}
	}
}

// A superuser reads as pg_read_all_data, so the powers that a read-only
// transaction leaves it are gone.
func TestSuperuserReadsAsPgReadAllData(t *testing.T) {
	databases := initDatabases(t)
	seed(t, databases)
	owner := databases.game
	client := connect(t, openBothAs(t, urlsOf(t, databases)))
	if role := call[QueryResult](t, client, "query", map[string]any{"database": GameDatabase, "sql": "SELECT current_user::text"}); role.Rows[0][0] != "pg_read_all_data" {
		t.Errorf("current_user = %v", role.Rows[0][0])
	}
	ownerPID := ownerValue(t, owner, "SELECT pg_backend_pid()::text")
	for _, statement := range []string{
		"SELECT pg_read_file('/etc/hostname')",
		"SELECT lo_export(0, '/tmp/mallory')",
		"SELECT pg_terminate_backend(" + ownerPID + ")",
		"SELECT pg_reload_conf()",
		"SELECT pg_create_physical_replication_slot('mallory', true)",
	} {
		if message := callFailing(t, client, "query", map[string]any{"database": GameDatabase, "sql": statement}); !strings.Contains(message, "not permitted") && !strings.Contains(message, "permission denied") {
			t.Errorf("%q: %s", statement, message)
		}
	}
	// pg_read_all_data still reads everything, so the typed tools work.
	if lobby := call[QueryResult](t, client, "query", map[string]any{"database": LobbyDatabase, "sql": "SELECT count(*) FROM lobby_sessions"}); lobby.Rows[0][0] != float64(1) {
		t.Errorf("sessions = %v", lobby.Rows)
	}
}

// When a migration outruns the read-only role's grants and the role can read
// bearer hashes, query is refused until a VM deploy grants them again. The
// typed tools never read the column, so they keep working.
func TestQueryIsRefusedWhileBearerHashesAreReadable(t *testing.T) {
	owners := initDatabases(t)
	seed(t, owners)
	owner := owners.lobby
	adminURLs := grantAdminRole(t, owners)
	// A migration that recreates the table, which the default privileges let
	// the role read whole.
	for _, statement := range []string{
		"ALTER TABLE lobby_sessions RENAME TO lobby_sessions_old",
		"CREATE TABLE lobby_sessions (LIKE lobby_sessions_old INCLUDING ALL)",
		"INSERT INTO lobby_sessions SELECT * FROM lobby_sessions_old",
	} {
		if err := owner.Exec(statement).Error; err != nil {
			t.Fatal(err)
		}
	}
	client := connect(t, openBothAs(t, adminURLs))
	if message := callFailing(t, client, "query", map[string]any{"database": LobbyDatabase, "sql": "SELECT 1"}); !strings.Contains(message, "VM deploy") {
		t.Errorf("query while bearer hashes are readable: %s", message)
	}
	// Island Battles' database holds no credentials, so query runs there.
	call[QueryResult](t, client, "query", map[string]any{"database": GameDatabase, "sql": "SELECT 1"})
	call[Schema](t, client, "describe_schema", map[string]any{"database": LobbyDatabase})
	if text, isError := client.callText(t, "overview", nil); isError {
		t.Errorf("overview failed: %s", text)
	}

	grantAdminRole(t, owners)
	if message := callFailing(t, client, "query", map[string]any{"database": LobbyDatabase, "sql": "SELECT bearer_hash FROM lobby_sessions"}); !strings.Contains(message, "name the columns") {
		t.Errorf("bearer_hash after the grants: %s", message)
	}
	call[QueryResult](t, client, "query", map[string]any{"database": LobbyDatabase, "sql": "SELECT count(*) FROM lobby_sessions"})
	if message := callFailing(t, client, "query", map[string]any{"database": LobbyDatabase, "sql": "SELECT * FROM pg_authid"}); !strings.Contains(message, "not permitted") || strings.Contains(message, "lobby_sessions") {
		t.Errorf("another table that the role may not read: %s", message)
	}
}

func TestLongValuesAreCut(t *testing.T) {
	client := connect(t, openBothAs(t, urlsOf(t, initDatabases(t))))
	result := call[QueryResult](t, client, "query", map[string]any{"database": GameDatabase, "sql": "SELECT repeat('é', 1000000) AS text, 'short' AS other"})
	if len(result.Rows) != 1 || !result.Truncated || len(result.Notes) != 1 || !strings.Contains(result.Notes[0], "16 KiB") {
		t.Fatalf("result: %d rows, truncated %t, notes %v", len(result.Rows), result.Truncated, result.Notes)
	}
	if text := result.Rows[0][0].(string); len(text) != maxValueBytes || strings.Trim(text, "é") != "" || result.Rows[0][1] != "short" {
		t.Errorf("row = %d bytes, %v", len(text), result.Rows[0][1])
	}

	// pgx returns json, jsonb and xml as bytes. A cut inside a character
	// leaves them text, not hex.
	documents := call[QueryResult](t, client, "query", map[string]any{"database": GameDatabase, "sql": `SELECT jsonb_build_object('k', repeat('é', 20000)) AS document,
		json_build_object('k', repeat('é', 20000)) AS json_document, xmlelement(name k, repeat('é', 20000)) AS markup`})
	for index, value := range documents.Rows[0] {
		text, ok := value.(string)
		if !ok || strings.HasPrefix(text, `\x`) || !utf8.ValidString(text) || len(text) > maxValueBytes || len(text) < maxValueBytes-3 || !strings.Contains(text, "éé") {
			t.Errorf("%s = %.40q (%d bytes), want text cut to 16 KiB", documents.Columns[index].Name, value, len(text))
		}
	}

	// One 16 KiB value of characters that HTML escaping would grow fits.
	markup := call[QueryResult](t, client, "query", map[string]any{"database": GameDatabase, "sql": "SELECT repeat('<', 16000) AS markup"})
	if len(markup.Rows) != 1 || markup.Truncated {
		t.Errorf("a value of < : %d rows, truncated %t, notes %v", len(markup.Rows), markup.Truncated, markup.Notes)
	}
}

// The production role can't write WAL through pg_logical_emit_message,
// which a rollback doesn't undo.
func TestAdminRoleCantEmitLogicalMessages(t *testing.T) {
	client := connect(t, openBothAs(t, grantAdminRole(t, initDatabases(t))))
	for _, statement := range []string{
		"SELECT pg_logical_emit_message(false, 'mallory', 'x')",
		"SELECT pg_logical_emit_message(false, 'mallory', 'x'::bytea)",
		"SELECT pg_logical_emit_message(false, 'mallory', 'x', true)",
	} {
		for _, database := range []string{GameDatabase, LobbyDatabase} {
			if message := callFailing(t, client, "query", map[string]any{"database": database, "sql": statement}); !strings.Contains(message, "permission denied") {
				t.Errorf("%q in %s: %s", statement, database, message)
			}
		}
	}
}

// Without bearer_hash, the column grants may no longer match the table, so
// query is refused, also for a role that may read everything.
func TestQueryIsRefusedWhenBearerHashIsRenamed(t *testing.T) {
	owner := initDatabases(t)
	if err := owner.lobby.Exec("ALTER TABLE lobby_sessions RENAME COLUMN bearer_hash TO credential_digest").Error; err != nil {
		t.Fatal(err)
	}
	client := connect(t, openBothAs(t, urlsOf(t, owner)))
	if message := callFailing(t, client, "query", map[string]any{"database": LobbyDatabase, "sql": "SELECT 1"}); !strings.Contains(message, "admin-grants-lobby.sql.j2") {
		t.Errorf("query without bearer_hash: %s", message)
	}
}

func TestGameRetentionFollowsTheBackendsSetting(t *testing.T) {
	owner := initDatabases(t)
	ids := seed(t, owner)
	client := connectWithRetention(t, openBothAs(t, urlsOf(t, owner)), 48*time.Hour)
	game := call[GameDetail](t, client, "get_game", map[string]any{"game_id": ids.runningGameID})
	if game.LastGenerationAt == nil || !game.RetentionDeletesAt.Equal(game.LastGenerationAt.Add(48*time.Hour)) {
		t.Errorf("retention deletes at %v, last generation at %v", game.RetentionDeletesAt, game.LastGenerationAt)
	}
}
