package dal_test

import (
	"testing"
	"time"

	"islandBattles/backend/dal"
	"islandBattles/backend/internal/testutils"
	"islandBattles/backend/models"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// A turn can commit a generation after RetireExpiredGames chose its
// candidates but before it locked them. The retirement checks again under
// the lock, so the game is kept. The test adds the generation from inside the
// pass's transaction, right after the candidate query, to hit that window.
func TestRetireExpiredGamesKeepsAGameThatGotAGenerationAfterItWasChosen(t *testing.T) {
	database := testutils.InitTestDB(t)
	generation, err := dal.GetGeneration(database, testutils.CreateGenerationForTest(t, database))
	if err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-8 * 24 * time.Hour)
	if err := database.Model(&models.Game{Model: gorm.Model{ID: generation.GameID}}).UpdateColumn("created_at", old).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.Model(generation).UpdateColumn("created_at", old).Error; err != nil {
		t.Fatal(err)
	}

	added := false
	callback := "test:turn_after_candidates"
	if err := database.Callback().Query().After("gorm:query").Register(callback, func(statement *gorm.DB) {
		if added || statement.Statement.Table != "games" {
			return
		}
		added = true
		turn := models.Generation{GameID: generation.GameID, MapID: generation.MapID}
		if err := statement.Session(&gorm.Session{NewDB: true}).Omit(clause.Associations).Create(&turn).Error; err != nil {
			t.Error(err)
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Callback().Query().Remove(callback) })

	retired, err := dal.RetireExpiredGames(database, time.Now().Add(-7*24*time.Hour), 10)
	if err != nil {
		t.Fatal(err)
	}
	if !added || retired != 0 {
		t.Fatalf("added a generation=%v retired=%d, want the game kept", added, retired)
	}
	if _, err := dal.FindGame(database, generation.GameID); err != nil {
		t.Fatalf("FindGame() error = %v, want the game visible", err)
	}
}
