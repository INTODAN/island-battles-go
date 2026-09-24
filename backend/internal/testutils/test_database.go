package testutils

import (
	"context"
	"islandBattles/backend/dependencies"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
	"gorm.io/gorm"
)

func InitTestDB(t *testing.T) *gorm.DB {
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
	// 2. Schedule Cleanup (Automatic Teardown)
	t.Cleanup(func() {
		if err := postgresContainer.Terminate(ctx); err != nil {
			t.Fatalf("failed to terminate container: %s", err)
		}
	})

	// 3. Get Connection String
	// "sslmode=disable" is required for local Docker connections usually
	connStr, err := postgresContainer.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("failed to get connection string: %s", err)
	}

	// 4. Connect with GORM
	// Note: We use the GORM driver here, not the testcontainers module
	db, err := dependencies.InitDB(connStr)
	if err != nil {
		t.Fatalf("initialize test database: %v", err)
	}
	sqlDatabase, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDatabase.Close() })
	return db
}
