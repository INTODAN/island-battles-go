package main

import (
	"context"
	"net/url"
	"strings"
	"testing"
	"time"

	"islandBattles/backend/dependencies"
	"islandBattles/backend/internal/testutils"

	"gorm.io/gorm"
)

// grantAdminRole runs the Ansible SQL for the production read-only role and
// returns the role's URL.
func grantAdminRole(t *testing.T, owner *gorm.DB) string {
	t.Helper()
	ownerURL, err := url.Parse(dsnOf(t, owner))
	if err != nil {
		t.Fatal(err)
	}
	script := adminRoleSQL(t, map[string]string{
		"postgres_admin_role": "island_battles_admin", "postgres_admin_password": "adminPassword123",
		"postgres_app_role": ownerURL.User.Username(), "postgres_database": strings.TrimPrefix(ownerURL.Path, "/"),
	})
	if err := owner.Transaction(func(transaction *gorm.DB) error { return transaction.Exec(script).Error }); err != nil {
		t.Fatal(err)
	}
	adminURL := *ownerURL
	adminURL.User = url.UserPassword("island_battles_admin", "adminPassword123")
	return adminURL.String()
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
// transaction, and the server keeps its one connection, so it must be
// released after each call. Otherwise it blocks the migrations.
func TestAdvisoryLocksEndWithTheCall(t *testing.T) {
	owner := testutils.InitTestDB(t)
	seed(t, owner)
	client := connect(t, openAs(t, grantAdminRole(t, owner)))
	call[QueryResult](t, client, "query", map[string]any{"sql": "SELECT pg_advisory_lock(hashtextextended('lobby_schema_migrations', 0)), pg_advisory_lock_shared(42)"})
	call[QueryResult](t, client, "query", map[string]any{"sql": "SELECT 1"})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := dependencies.MigrateSchema(ctx, owner.WithContext(ctx)); err != nil {
		t.Fatalf("the migrations waited for the admin server's lock: %v", err)
	}
	if locked := ownerValue(t, owner, "SELECT pg_try_advisory_lock(42)::text"); locked != "true" {
		t.Errorf("another session can't take lock 42 after the call")
	}
	if held := ownerValue(t, owner, "SELECT count(*)::text FROM pg_locks WHERE locktype = 'advisory' AND pid <> pg_backend_pid()"); held != "0" {
		t.Errorf("%s advisory locks are still held", held)
	}
}

// A superuser reads as pg_read_all_data, so the powers that a read-only
// transaction leaves it are gone.
func TestSuperuserReadsAsPgReadAllData(t *testing.T) {
	owner := testutils.InitTestDB(t)
	seed(t, owner)
	client := connect(t, openAs(t, dsnOf(t, owner)))
	if role := call[QueryResult](t, client, "query", map[string]any{"sql": "SELECT current_user::text"}); role.Rows[0][0] != "pg_read_all_data" {
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
		if message := callFailing(t, client, "query", map[string]any{"sql": statement}); !strings.Contains(message, "not permitted") && !strings.Contains(message, "permission denied") {
			t.Errorf("%q: %s", statement, message)
		}
	}
	// pg_read_all_data still reads everything, so the typed tools work.
	if lobby := call[QueryResult](t, client, "query", map[string]any{"sql": "SELECT count(*) FROM lobby_sessions"}); lobby.Rows[0][0] != float64(1) {
		t.Errorf("sessions = %v", lobby.Rows)
	}
}

// When a migration outruns the read-only role's grants and the role can read
// bearer hashes, query is refused until a VM deploy grants them again. The
// typed tools never read the column, so they keep working.
func TestQueryIsRefusedWhileBearerHashesAreReadable(t *testing.T) {
	owner := testutils.InitTestDB(t)
	seed(t, owner)
	adminURL := grantAdminRole(t, owner)
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
	client := connect(t, openAs(t, adminURL))
	if message := callFailing(t, client, "query", map[string]any{"sql": "SELECT 1"}); !strings.Contains(message, "VM deploy") {
		t.Errorf("query while bearer hashes are readable: %s", message)
	}
	call[Schema](t, client, "describe_schema", nil)
	if text, isError := client.callText(t, "overview", nil); isError {
		t.Errorf("overview failed: %s", text)
	}

	grantAdminRole(t, owner)
	if message := callFailing(t, client, "query", map[string]any{"sql": "SELECT bearer_hash FROM lobby_sessions"}); !strings.Contains(message, "name the columns") {
		t.Errorf("bearer_hash after the grants: %s", message)
	}
	call[QueryResult](t, client, "query", map[string]any{"sql": "SELECT count(*) FROM lobby_sessions"})
	if message := callFailing(t, client, "query", map[string]any{"sql": "SELECT * FROM pg_authid"}); !strings.Contains(message, "not permitted") || strings.Contains(message, "lobby_sessions") {
		t.Errorf("another table that the role may not read: %s", message)
	}
}

func TestLongValuesAreCut(t *testing.T) {
	owner := testutils.InitTestDB(t)
	client := connect(t, openAs(t, dsnOf(t, owner)))
	result := call[QueryResult](t, client, "query", map[string]any{"sql": "SELECT repeat('é', 1000000) AS text, 'short' AS other"})
	if len(result.Rows) != 1 || !result.Truncated || len(result.Notes) != 1 || !strings.Contains(result.Notes[0], "16 KiB") {
		t.Fatalf("result: %d rows, truncated %t, notes %v", len(result.Rows), result.Truncated, result.Notes)
	}
	if text := result.Rows[0][0].(string); len(text) != maxValueBytes || strings.Trim(text, "é") != "" || result.Rows[0][1] != "short" {
		t.Errorf("row = %d bytes, %v", len(text), result.Rows[0][1])
	}
}

func TestGameRetentionFollowsTheBackendsSetting(t *testing.T) {
	owner := testutils.InitTestDB(t)
	ids := seed(t, owner)
	client := connectWithRetention(t, openAs(t, dsnOf(t, owner)), 48*time.Hour)
	game := call[GameDetail](t, client, "get_game", map[string]any{"game_id": ids.runningGameID})
	if game.LastGenerationAt == nil || !game.RetentionDeletesAt.Equal(game.LastGenerationAt.Add(48*time.Hour)) {
		t.Errorf("retention deletes at %v, last generation at %v", game.RetentionDeletesAt, game.LastGenerationAt)
	}
}
