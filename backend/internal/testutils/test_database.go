package testutils

import (
	"context"
	"islandBattles/backend/dependencies"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
	gormpostgres "gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// InitTestDB starts a disposable PostgreSQL container with the full schema.
func InitTestDB(t *testing.T) *gorm.DB {
	db, err := dependencies.InitDB(startTestPostgres(t))
	if err != nil {
		t.Fatalf("initialize test database: %v", err)
	}
	closeWithTest(t, db)
	return db
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
