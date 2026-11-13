package testutils

import (
	"context"
	"islandBattles/backend/dependencies"
	"islandBattles/backend/schema"
	"log/slog"
	"net/url"
	"testing"
	"time"

	lobbypostgres "islandBattles/lobby/postgres"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
	gormpostgres "gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// LobbyTestDatabase is the name of the lobby's database in the test server,
// beside Island Battles' own, as on the VM (#229).
const LobbyTestDatabase = "lobby"

// InitTestDB starts a disposable PostgreSQL container with Island Battles'
// full schema. It connects as the hosts do, with the default pool limits,
// and migrates as the migrate command does. The lobby's tables are in a
// database of their own; see InitTestDatabases.
func InitTestDB(t *testing.T) *gorm.DB {
	game, _ := InitTestDatabases(t)
	return game
}

// InitTestDatabases starts a disposable PostgreSQL container with two
// databases on it, as on the VM: Island Battles', migrated as its migrate
// command does, and the lobby's, migrated as the lobby's migrate command
// does. Both connect as the hosts do, with the default pool limits.
func InitTestDatabases(t *testing.T) (game, lobby *gorm.DB) {
	gameURL, lobbyURL := StartTestDatabases(t)
	game = connectTestDatabase(t, gameURL)
	if err := schema.Migrate(context.Background(), game); err != nil {
		t.Fatalf("migrate test database: %v", err)
	}
	lobby = connectTestDatabase(t, lobbyURL)
	if err := lobbypostgres.Migrate(context.Background(), lobby); err != nil {
		t.Fatalf("migrate the lobby's test database: %v", err)
	}
	return game, lobby
}

// AddLobbyTablesForTest gives Island Battles' database the lobby's tables, as
// builds before the lobby's own database (#229) kept them there.
func AddLobbyTablesForTest(t *testing.T, game *gorm.DB) {
	t.Helper()
	if err := lobbypostgres.Migrate(context.Background(), game); err != nil {
		t.Fatalf("add the lobby's tables: %v", err)
	}
}

// InitEmptyTestDB starts a disposable PostgreSQL container without any schema.
func InitEmptyTestDB(t *testing.T) *gorm.DB {
	db, err := gorm.Open(gormpostgres.Open(startTestPostgres(t)), &gorm.Config{})
	if err != nil {
		t.Fatalf("open empty test database: %v", err)
	}
	closeWithTest(t, db)
	return db
}

// StartTestDatabases starts a disposable PostgreSQL container with two empty
// databases, Island Battles' and the lobby's (LobbyTestDatabase), and returns
// their connection strings.
func StartTestDatabases(t *testing.T) (gameURL, lobbyURL string) {
	gameURL = startTestPostgres(t)
	server, err := gorm.Open(gormpostgres.Open(gameURL), &gorm.Config{})
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	closeWithTest(t, server)
	if err := server.Exec(`CREATE DATABASE ?`, clause.Table{Name: LobbyTestDatabase}).Error; err != nil {
		t.Fatalf("create the lobby's test database: %v", err)
	}
	parsed, err := url.Parse(gameURL)
	if err != nil {
		t.Fatal(err)
	}
	parsed.Path = "/" + LobbyTestDatabase
	return gameURL, parsed.String()
}

func connectTestDatabase(t *testing.T, databaseURL string) *gorm.DB {
	db, err := dependencies.InitDB(dependencies.DefaultDatabaseConfig(databaseURL), slog.Default())
	if err != nil {
		t.Fatalf("connect test database: %v", err)
	}
	closeWithTest(t, db)
	return db
}

// startTestPostgres returns the connection string of a new container that is
// terminated when the test ends.
func startTestPostgres(t *testing.T) string {
	ctx := context.Background()

	postgresContainer, err := postgres.Run(ctx, "postgres:18-alpine",
		postgres.WithDatabase("islandBattles"), postgres.WithUsername("islandBattles"), postgres.WithPassword("islandBattles"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(5*time.Second),
		))
	if err != nil {
		t.Fatalf("Failed to start postgres container: %s", err)
	}
	t.Cleanup(func() {
		if err := postgresContainer.Terminate(ctx); err != nil {
			t.Fatalf("failed to terminate container: %s", err)
		}
	})

	// "sslmode=disable" is required for local Docker connections usually
	connStr, err := postgresContainer.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("failed to get connection string: %s", err)
	}
	return connStr
}

func closeWithTest(t *testing.T, db *gorm.DB) {
	sqlDatabase, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDatabase.Close() })
}
