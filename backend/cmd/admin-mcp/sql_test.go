package main

import (
	"context"
	"net/url"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"islandBattles/backend/internal/testutils"
	"islandBattles/backend/models"
	"islandBattles/lobby/adminmcp"
	lobbypostgres "islandBattles/lobby/postgres"

	"gorm.io/gorm"
)

func TestQueryReadsOneStatement(t *testing.T) {
	owner := testutils.InitTestDB(t)
	ids := seed(t, owner)
	client := connect(t, openAs(t, dsnOf(t, owner)))

	t.Run("values keep their types", func(t *testing.T) {
		result := call[QueryResult](t, client, "query", map[string]any{"sql": `SELECT id, phase, capacity, game_settings, created_at,
			'\x0102'::bytea AS raw, 1.5::numeric AS amount, 'NaN'::float8 AS not_a_number, NULL AS nothing
			FROM lobby_rooms -- a trailing comment`})
		if len(result.Rows) != 1 || result.Truncated || len(result.Columns) != 9 {
			t.Fatalf("result = %+v", result)
		}
		row := result.Rows[0]
		if row[0] != ids.lobbyID || row[1] != "started" || row[2] != float64(2) || row[5] != `\x0102` || row[6] != "1.5" || row[7] != "NaN" || row[8] != nil {
			t.Errorf("row = %#v", row)
		}
		if settings, ok := row[3].(map[string]any); !ok || len(settings) != 0 {
			t.Errorf("game_settings = %#v, want the JSON object", row[3])
		}
		if createdAt, err := time.Parse(time.RFC3339Nano, row[4].(string)); err != nil || createdAt.Location() != time.UTC {
			t.Errorf("created_at = %v, want RFC 3339 in UTC", row[4])
		}
		if result.Columns[0] != (QueryColumn{Name: "id", Type: "uuid"}) {
			t.Errorf("columns = %+v", result.Columns)
		}
		withQuery := call[QueryResult](t, client, "query", map[string]any{"sql": "WITH games_per_lobby AS (SELECT lobby_id FROM games) SELECT count(*) FROM games_per_lobby;"})
		if len(withQuery.Rows) != 1 || withQuery.Rows[0][0] != float64(2) {
			t.Errorf("WITH query = %+v", withQuery)
		}
	})

	t.Run("results are cut", func(t *testing.T) {
		long := call[QueryResult](t, client, "query", map[string]any{"sql": "SELECT n FROM generate_series(1, 100000) AS n"})
		if len(long.Rows) != maxQueryRows || !long.Truncated || len(long.Notes) != 1 || !strings.Contains(long.Notes[0], "200 rows") {
			t.Errorf("long result: %d rows, truncated %t, notes %v", len(long.Rows), long.Truncated, long.Notes)
		}
		// Each row is 1,004 bytes of JSON and a separator, so 65 fit in 64 KiB.
		wide := call[QueryResult](t, client, "query", map[string]any{"sql": "SELECT repeat('x', 1000) FROM generate_series(1, 100)"})
		if len(wide.Rows) != 65 || !wide.Truncated || len(wide.Notes) != 1 || !strings.Contains(wide.Notes[0], "64 KiB") {
			t.Errorf("wide result: %d rows, truncated %t, notes %v", len(wide.Rows), wide.Truncated, wide.Notes)
		}
		exact := call[QueryResult](t, client, "query", map[string]any{"sql": "SELECT n FROM generate_series(1, 200) AS n"})
		if len(exact.Rows) != maxQueryRows || exact.Truncated {
			t.Errorf("200 rows: %d rows, truncated %t", len(exact.Rows), exact.Truncated)
		}
	})

	t.Run("writes and tricks fail", func(t *testing.T) {
		for _, statement := range []string{
			"INSERT INTO lobby_players (id, display_name, created_at) VALUES (gen_random_uuid(), 'Mallory', now())",
			"UPDATE lobby_rooms SET name = 'Mallory'",
			"DELETE FROM lobby_sessions",
			"WITH deleted AS (DELETE FROM lobby_sessions RETURNING id) SELECT count(*) FROM deleted",
			"CREATE TABLE mallory (id int)",
			"CREATE TEMPORARY TABLE mallory (id int)",
			"DROP TABLE games",
			"TRUNCATE games",
			"SET TRANSACTION READ WRITE",
			"SET default_transaction_read_only = off",
			"SET statement_timeout = 0",
			"COPY lobby_rooms TO STDOUT",
			"COPY (SELECT 1) TO '/tmp/mallory'",
			"COPY (SELECT 1) TO PROGRAM 'true'",
			"COMMIT",
			"SELECT 1; SELECT 2",
			"SELECT 1; UPDATE lobby_rooms SET name = 'Mallory'",
			"SELECT * FROM lobby_rooms FOR UPDATE",
			"SELECT nextval('games_id_seq')",
			"SELECT lo_create(0)",
			"SELECT pg_sleep(10)",
			"SELECT set_config('statement_timeout', '0', false), pg_sleep(10)",
			"EXPLAIN ANALYZE UPDATE lobby_rooms SET name = 'Mallory'",
			"",
		} {
			started := time.Now()
			message := callFailing(t, client, "query", map[string]any{"sql": statement})
			if elapsed := time.Since(started); elapsed > 8*time.Second {
				t.Errorf("%q took %v to fail", statement, elapsed)
			}
			t.Logf("%q: %s", statement, message)
		}
		if message := callFailing(t, client, "query", map[string]any{"sql": "SELECT 1; SELECT 2"}); !strings.Contains(message, "exactly one statement") {
			t.Errorf("several statements: %s", message)
		}
		if message := callFailing(t, client, "query", map[string]any{"sql": "SELECT pg_sleep(10)"}); !strings.Contains(message, "5s statement timeout") {
			t.Errorf("a long statement: %s", message)
		}
		if message := callFailing(t, client, "query", map[string]any{"sql": "SELECT lo_create(0)"}); !strings.Contains(message, "read-only") {
			t.Errorf("a write in a query: %s", message)
		}
		var names []string
		if err := owner.Model(&lobbypostgres.RoomRecord{}).Pluck("name", &names).Error; err != nil || len(names) != 1 || names[0] != "Battle" {
			t.Errorf("lobby names = %v, %v; want them unchanged", names, err)
		}
		var games int64
		if err := owner.Model(&models.Game{}).Count(&games).Error; err != nil || games != 2 {
			t.Errorf("games = %d, %v; want 2", games, err)
		}
		// The session is still read-only with its timeout afterwards.
		settings := call[QueryResult](t, client, "query", map[string]any{"sql": "SELECT current_setting('default_transaction_read_only'), current_setting('statement_timeout')"})
		if settings.Rows[0][0] != "on" || settings.Rows[0][1] != "5s" {
			t.Errorf("settings after the tricks = %v", settings.Rows)
		}
	})
}

// The read-only transaction refuses writes on its own, without the cursor
// that query reads through.
func TestToolTransactionsRefuseWrites(t *testing.T) {
	owner := testutils.InitTestDB(t)
	seed(t, owner)
	database := openAs(t, dsnOf(t, owner))
	for _, statement := range []string{
		"INSERT INTO lobby_players (id, display_name, created_at) VALUES (gen_random_uuid(), 'Mallory', now())",
		"UPDATE lobby_rooms SET name = 'Mallory'",
		"DELETE FROM lobby_sessions",
		"CREATE TABLE mallory (id int)",
		"CREATE TEMPORARY TABLE mallory (id int)",
		"TRUNCATE games",
	} {
		err := adminmcp.ReadOnly(context.Background(), database, func(transaction *gorm.DB) error {
			sqlTransaction, err := adminmcp.SQLTransaction(transaction)
			if err != nil {
				return err
			}
			_, err = sqlTransaction.ExecContext(context.Background(), statement)
			return err
		})
		if err == nil || !strings.Contains(err.Error(), "read-only transaction") {
			t.Errorf("%q in a read-only transaction: %v", statement, err)
		}
	}
	var names []string
	if err := owner.Model(&lobbypostgres.RoomRecord{}).Pluck("name", &names).Error; err != nil || len(names) != 1 || names[0] != "Battle" {
		t.Errorf("lobby names = %v, %v; want them unchanged", names, err)
	}
}

// adminRoleSQL renders the Ansible template that creates the production
// read-only role. It uses only plain substitutions.
func adminRoleSQL(t *testing.T, variables map[string]string) string {
	t.Helper()
	template, err := os.ReadFile("../../../deploy/ansible/roles/app/templates/admin-role.sql.j2")
	if err != nil {
		t.Fatal(err)
	}
	rendered := regexp.MustCompile(`\{\{ ([a-z_]+) \}\}`).ReplaceAllStringFunc(string(template), func(placeholder string) string {
		name := strings.Trim(placeholder, "{} ")
		value, found := variables[name]
		if !found {
			t.Fatalf("the template uses %s, which the test doesn't set", name)
		}
		return value
	})
	if strings.Contains(rendered, "{{") || strings.Contains(rendered, "{%") {
		t.Fatalf("the template uses more than plain substitutions: %s", rendered)
	}
	return rendered
}

// The production role, as Ansible creates it, reads every table and every
// lobby_sessions column except bearer_hash, also in tables created after it.
func TestAdminRoleReadsAllButBearerHashes(t *testing.T) {
	owner := testutils.InitTestDB(t)
	ids := seed(t, owner)
	ownerURL, err := url.Parse(dsnOf(t, owner))
	if err != nil {
		t.Fatal(err)
	}
	script := adminRoleSQL(t, map[string]string{
		"postgres_admin_role": "island_battles_admin", "postgres_admin_password": "adminPassword123",
		"postgres_app_role": ownerURL.User.Username(), "postgres_database": strings.TrimPrefix(ownerURL.Path, "/"),
	})
	// Ansible runs it in one transaction, and every deploy runs it again.
	for range 2 {
		if err := owner.Transaction(func(transaction *gorm.DB) error { return transaction.Exec(script).Error }); err != nil {
			t.Fatal(err)
		}
	}
	// A table that a later migration creates.
	if err := owner.Exec("CREATE TABLE later_game_notes (id bigserial PRIMARY KEY, message text NOT NULL)").Error; err != nil {
		t.Fatal(err)
	}
	if err := owner.Exec("INSERT INTO later_game_notes (message) VALUES ('hello')").Error; err != nil {
		t.Fatal(err)
	}
	adminURL := *ownerURL
	adminURL.User = url.UserPassword("island_battles_admin", "adminPassword123")
	client := connect(t, openAs(t, adminURL.String()))

	overview := call[adminmcp.Overview](t, client, "overview", nil)
	if overview.LiveSessionCount != 1 {
		t.Errorf("live sessions = %d, want Alice's", overview.LiveSessionCount)
	}
	if players := call[adminmcp.PlayerList](t, client, "find_players", map[string]any{"player_id": ids.alice}); len(players.Players) != 1 || len(players.Players[0].LiveSessions) != 1 {
		t.Errorf("Alice = %+v, want her live session", players)
	}
	for _, tool := range []struct {
		name      string
		arguments map[string]any
	}{
		{"list_lobbies", nil}, {"get_lobby", map[string]any{"lobby_id": ids.lobbyID}}, {"find_players", map[string]any{"display_name": "a"}},
		{"list_games", nil}, {"get_game", map[string]any{"game_id": ids.runningGameID}},
		{"get_generation", map[string]any{"game_id": ids.runningGameID, "turn": 2, "include_board": true}},
		{"describe_schema", nil}, {"describe_schema", map[string]any{"table": "lobby_sessions"}},
		{"query", map[string]any{"sql": "SELECT id, player_id, created_at, expires_at FROM lobby_sessions"}},
		{"query", map[string]any{"sql": "SELECT count(*) FROM lobby_sessions"}},
		{"query", map[string]any{"sql": "SELECT message FROM later_game_notes"}},
	} {
		if text, isError := client.callText(t, tool.name, tool.arguments); isError {
			t.Errorf("%s(%v) failed: %s", tool.name, tool.arguments, text)
		}
	}
	for _, statement := range []string{
		"SELECT * FROM lobby_sessions",
		"SELECT bearer_hash FROM lobby_sessions",
		"SELECT s FROM lobby_sessions AS s",
		"SELECT encode(bearer_hash, 'hex') FROM lobby_sessions",
	} {
		if message := callFailing(t, client, "query", map[string]any{"sql": statement}); !strings.Contains(message, "permission denied") {
			t.Errorf("%q: %s", statement, message)
		}
	}
	// The role has no write privileges even outside the server's read-only
	// sessions. The server keeps one connection, so both statements use it.
	adminDatabase := openAs(t, adminURL.String())
	if err := adminDatabase.Exec("SET default_transaction_read_only = off").Error; err != nil {
		t.Fatal(err)
	}
	player := lobbypostgres.PlayerRecord{ID: "00000000-0000-0000-0000-000000000001", DisplayName: "Mallory", CreatedAt: time.Now()}
	if err := adminDatabase.Create(&player).Error; err == nil || !strings.Contains(err.Error(), "permission denied") {
		t.Errorf("the admin role inserted: %v", err)
	}
	checkNoBearerHash(t, client, true)
}
