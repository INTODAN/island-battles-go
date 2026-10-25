package dependencies_test

import (
	"testing"

	"islandBattles/backend/dal"
	"islandBattles/backend/dependencies"
	"islandBattles/backend/internal/testutils"
	"islandBattles/backend/models"

	gormpostgres "gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// restartDB runs startup again against the database behind db.
func restartDB(t *testing.T, db *gorm.DB) *gorm.DB {
	t.Helper()
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
	return restarted
}

// Generation events are pushed after commit, so startup drops the table that
// queued them in databases created before.
func TestInitDBDropsTheGenerationNotificationQueue(t *testing.T) {
	db := testutils.InitTestDB(t)
	if err := db.Exec(`CREATE TABLE generation_notifications (id bigserial PRIMARY KEY, payload jsonb)`).Error; err != nil {
		t.Fatal(err)
	}
	if restartDB(t, db).Migrator().HasTable("generation_notifications") {
		t.Fatal("generation_notifications survived startup")
	}
}

// gameWithReplay is the games table from before replays were derived from
// generations.
type gameWithReplay struct {
	gorm.Model
	LobbyID            *string `gorm:"type:uuid;uniqueIndex"`
	Replay             []byte  `gorm:"type:jsonb;not null"`
	NextSoldierGroupID uint    `gorm:"not null"`
}

func (gameWithReplay) TableName() string { return "games" }

func TestInitDBDropsTheOldReplayColumn(t *testing.T) {
	db := testutils.InitEmptyTestDB(t)
	if err := db.AutoMigrate(&gameWithReplay{}); err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&gameWithReplay{Replay: []byte(`{"frames":[]}`)}).Error; err != nil {
		t.Fatal(err)
	}
	restartDB(t, db)
	restarted := restartDB(t, db)
	if restarted.Migrator().HasColumn(&models.Game{}, "replay") {
		t.Fatal("games still has the replay column")
	}
	if _, err := dal.CreateGame(restarted); err != nil {
		t.Fatalf("new games cannot be stored: %v", err)
	}
}
