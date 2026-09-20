package migrations_test

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	containerpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"islandBattles/backend/migrations"
	lobbypostgres "islandBattles/lobby/postgres"
)

func migrationDatabase(t *testing.T) *gorm.DB {
	t.Helper()
	ctx := context.Background()
	container, err := containerpostgres.Run(ctx, "postgres:18-alpine", containerpostgres.WithDatabase("lobby_migration_test"), containerpostgres.WithUsername("test"), containerpostgres.WithPassword("test"), testcontainers.WithWaitStrategy(wait.ForLog("database system is ready to accept connections").WithOccurrence(2).WithStartupTimeout(time.Minute)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := container.Terminate(context.Background()); err != nil {
			t.Errorf("terminate: %v", err)
		}
	})
	connection, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	database, err := gorm.Open(postgres.Open(connection), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	sqlDatabase, err := database.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDatabase.Close() })
	if err := lobbypostgres.InitializeSchema(ctx, database); err != nil {
		t.Fatal(err)
	}
	return database
}

func seedLegacy(t *testing.T, database *gorm.DB) {
	t.Helper()
	statements := []string{
		`CREATE TABLE games(id bigint PRIMARY KEY, room_name text UNIQUE NOT NULL DEFAULT 'test-game', created_at timestamptz, updated_at timestamptz, deleted_at timestamptz)`,
		`CREATE TABLE players(id bigint PRIMARY KEY, game_id bigint REFERENCES games(id), team_color integer, created_at timestamptz, updated_at timestamptz, deleted_at timestamptz)`,
		`CREATE TABLE generations(id bigint PRIMARY KEY, game_id bigint REFERENCES games(id))`,
		`CREATE TABLE maps(id bigint PRIMARY KEY, generation_id bigint REFERENCES generations(id))`,
		`CREATE TABLE castles(id bigint PRIMARY KEY, map_id bigint REFERENCES maps(id), team_color integer, soldier_amount integer)`,
		`CREATE TABLE soldier_groups(id bigint PRIMARY KEY, generation_id bigint REFERENCES generations(id), team_color integer, soldier_amount integer)`,
		`CREATE TABLE commands(id bigint PRIMARY KEY, generation_id bigint REFERENCES generations(id), payload jsonb)`,
		`INSERT INTO games VALUES (1, 'legacy-room', '2020-01-01T01:02:03.123456Z', '2020-02-01Z', NULL), (2, 'deleted-room', '2020-01-01Z', '2020-02-01Z', '2020-03-01Z')`,
		`INSERT INTO players VALUES (9007199254740993,1,-7,'2020-01-01Z','2020-02-01Z',NULL),(2,1,-7,'2020-01-01Z','2020-02-01Z','2020-03-01Z')`,
		`INSERT INTO generations VALUES(3,1)`,
		`INSERT INTO maps VALUES(4,3)`,
		`INSERT INTO castles VALUES(5,4,1,99)`,
		`INSERT INTO soldier_groups VALUES(6,3,2,27)`,
		`INSERT INTO commands VALUES(7,3,'{"action":"move"}')`,
	}
	for _, statement := range statements {
		if err := database.Exec(statement).Error; err != nil {
			t.Fatal(err)
		}
	}
}

func scalar(t *testing.T, database *gorm.DB, query string) string {
	t.Helper()
	var result string
	if err := database.Raw(query).Scan(&result).Error; err != nil {
		t.Fatal(err)
	}
	return result
}

func TestLegacyMigrationPreservesGameGraph(t *testing.T) {
	database := migrationDatabase(t)
	seedLegacy(t, database)
	before := map[string]string{}
	for _, table := range []string{"players", "generations", "maps", "castles", "soldier_groups", "commands"} {
		before[table] = scalar(t, database, "SELECT jsonb_agg(to_jsonb(records) ORDER BY id)::text FROM "+table+" records")
	}
	report, err := migrations.MigrateLegacy(context.Background(), database)
	if err != nil {
		t.Fatal(err)
	}
	if report.Players != 2 || report.Games != 2 || report.AlreadyApplied {
		t.Fatalf("wrong report: %+v", report)
	}
	for table, expected := range before {
		if actual := scalar(t, database, "SELECT jsonb_agg(to_jsonb(records) ORDER BY id)::text FROM "+table+" records"); actual != expected {
			t.Fatalf("%s changed: %s", table, actual)
		}
	}
	if scalar(t, database, "SELECT count(*)::text FROM games WHERE lobby_id IS NOT NULL") != "0" {
		t.Fatal("invented lobby link")
	}
	for _, table := range []string{"lobby_players", "lobby_sessions", "lobby_rooms", "lobby_memberships"} {
		if scalar(t, database, "SELECT count(*)::text FROM "+table) != "0" {
			t.Fatalf("invented identities in %s", table)
		}
	}
	if err := migrations.VerifyArchive(context.Background(), database); err != nil {
		t.Fatal(err)
	}
	archive := scalar(t, database, "SELECT original_row_json::text FROM lobby_legacy_records WHERE source_table='players' AND source_primary_key='9007199254740993'")
	if !strings.Contains(archive, "9007199254740993") || !strings.Contains(archive, `"team_color": -7`) {
		t.Fatalf("archive lost integer/color: %s", archive)
	}
	if scalar(t, database, "SELECT count(*)::text FROM lobby_legacy_records WHERE source_deleted_at IS NOT NULL") != "2" {
		t.Fatal("soft-deleted records missing")
	}
	if scalar(t, database, "SELECT room_name FROM games WHERE id=1") != "legacy-room" {
		t.Fatal("legacy room name lost")
	}
}

func TestLegacyMigrationIsRepeatable(t *testing.T) {
	database := migrationDatabase(t)
	seedLegacy(t, database)
	first, err := migrations.MigrateLegacy(context.Background(), database)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.Exec("INSERT INTO games(id) VALUES(100),(101)").Error; err != nil {
		t.Fatal(err)
	}
	second, err := migrations.MigrateLegacy(context.Background(), database)
	if err != nil {
		t.Fatal(err)
	}
	if !second.AlreadyApplied || second.Players != first.Players || second.Games != first.Games || second.Checksum != first.Checksum {
		t.Fatalf("not repeatable: %+v / %+v", first, second)
	}
	if scalar(t, database, "SELECT count(*)::text FROM lobby_legacy_records") != "4" {
		t.Fatal("post-cutover game archived")
	}
	if scalar(t, database, "SELECT count(*)::text FROM games WHERE room_name IS NULL AND lobby_id IS NULL") != "2" {
		t.Fatal("detached game creation failed")
	}
}

func TestLegacyMigrationRejectsConflictingArchiveAndRetries(t *testing.T) {
	database := migrationDatabase(t)
	seedLegacy(t, database)
	if err := database.Exec(`INSERT INTO lobby_legacy_records(id,source_table,source_primary_key,original_row_json,checksum,archived_at,migration_version) VALUES('aaaaaaaa-aaaa-4aaa-aaaa-aaaaaaaaaaaa','games','1','{}','wrong',NOW(),1)`).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := migrations.MigrateLegacy(context.Background(), database); err == nil {
		t.Fatal("conflicting archive accepted")
	}
	if scalar(t, database, "SELECT count(*)::text FROM lobby_legacy_records") != "1" {
		t.Fatal("partial player import leaked after rollback")
	}
	if err := database.Exec("DELETE FROM lobby_legacy_records").Error; err != nil {
		t.Fatal(err)
	}
	if _, err := migrations.MigrateLegacy(context.Background(), database); err != nil {
		t.Fatal(err)
	}
	if scalar(t, database, "SELECT count(*)::text FROM lobby_legacy_records") != "4" {
		t.Fatal("retry incomplete")
	}
	if err := database.Exec("UPDATE lobby_legacy_records SET original_row_json='{}' WHERE source_table='players'").Error; err != nil {
		t.Fatal(err)
	}
	if err := migrations.VerifyArchive(context.Background(), database); err == nil {
		t.Fatal("archive corruption undetected")
	}
}

func TestLegacyMigrationConcurrentStartup(t *testing.T) {
	database := migrationDatabase(t)
	seedLegacy(t, database)
	barrier := make(chan struct{})
	failures := make(chan error, 2)
	var workers sync.WaitGroup
	for range 2 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-barrier
			_, err := migrations.MigrateLegacy(context.Background(), database)
			failures <- err
		}()
	}
	close(barrier)
	workers.Wait()
	close(failures)
	for err := range failures {
		if err != nil {
			t.Fatal(err)
		}
	}
	if scalar(t, database, "SELECT count(*)::text FROM lobby_legacy_records") != "4" {
		t.Fatal("duplicate/incomplete migration")
	}
}

func TestLegacyMigrationFreshDatabase(t *testing.T) {
	database := migrationDatabase(t)
	report, err := migrations.MigrateLegacy(context.Background(), database)
	if err != nil || report.Players != 0 || report.Games != 0 {
		t.Fatalf("unexpected fresh result: %+v %v", report, err)
	}
	second, err := migrations.MigrateLegacy(context.Background(), database)
	if err != nil || !second.AlreadyApplied {
		t.Fatalf("fresh marker not retained: %+v %v", second, err)
	}
}
