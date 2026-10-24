package dependencies_test

import (
	"testing"

	"islandBattles/backend/dependencies"
	"islandBattles/backend/internal/testutils"

	gormpostgres "gorm.io/driver/postgres"
)

// Generation events are pushed after commit, so startup drops the table that
// queued them in databases created before.
func TestInitDBDropsTheGenerationNotificationQueue(t *testing.T) {
	db := testutils.InitTestDB(t)
	if err := db.Exec(`CREATE TABLE generation_notifications (id bigserial PRIMARY KEY, payload jsonb)`).Error; err != nil {
		t.Fatal(err)
	}
	dialector, ok := db.Dialector.(*gormpostgres.Dialector)
	if !ok {
		t.Fatalf("unexpected dialector %T", db.Dialector)
	}
	restarted, err := dependencies.InitDB(dialector.DSN)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if sqlDatabase, err := restarted.DB(); err == nil {
			_ = sqlDatabase.Close()
		}
	})
	if restarted.Migrator().HasTable("generation_notifications") {
		t.Fatal("generation_notifications survived startup")
	}
}
