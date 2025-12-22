package dal_test

import (
	"islandBattles/backend/dal"
	"islandBattles/backend/internal/testutils"
	"testing"
)

func TestGetGeneration(t *testing.T) {
	db := testutils.InitTestDB(t)
	_, err := dal.GetGeneration(db, testutils.CreateGenerationForTest(t, db))
	if err != nil {
		t.Fatal(err)
	}
}
