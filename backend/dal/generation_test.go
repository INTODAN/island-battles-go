package dal_test

import (
	"islandBattles/backend/dal"
	"islandBattles/backend/internal/testutils"
	"islandBattles/backend/models"
	"islandBattles/backend/services"
	"testing"
)

func TestGetGeneration(t *testing.T) {
	db := testutils.InitTestDB(t)
	_, err := dal.GetGeneration(db, testutils.CreateGenerationForTest(t, db))
	if err != nil {
		t.Fatal(err)
	}
}

func TestGetLatestGeneration(t *testing.T) {
	db := testutils.InitTestDB(t)

	generationID := testutils.CreateGenerationForTest(t, db)
	generation, err := dal.GetGeneration(db, generationID)
	if err != nil {
		t.Fatal(err)
	}
	latestGeneration, err := dal.GetLatestGenerationForGame(db, generation.GameID)
	if err != nil {
		t.Fatal(err)
	}
	if latestGeneration.ID != generationID {
		t.Fatal("Latest generation ID not correct")
	}
	newGenerationID, err := services.MigrateGeneration(db, generation.GameID, []models.CommandFields{})
	if err != nil {
		t.Fatal(err)
	}
	latestGeneration, err = dal.GetLatestGenerationForGame(db, generation.GameID)
	if err != nil {
		t.Fatal(err)
	}
	if latestGeneration.ID != newGenerationID {
		t.Fatal("Latest generation ID not correct")
	}
}

// A generation's Number is its turn number within its own game, from 1,
// although the games' generations share one sequence of IDs.
func TestGenerationsAreNumberedWithinTheirGame(t *testing.T) {
	db := testutils.InitTestDB(t)
	first, err := dal.GetGeneration(db, testutils.CreateGenerationForTest(t, db))
	if err != nil {
		t.Fatal(err)
	}
	other, err := dal.GetGeneration(db, testutils.CreateComplexGenerationForTest(t, db))
	if err != nil {
		t.Fatal(err)
	}
	if first.Number != 1 || other.Number != 1 || other.ID == 1 {
		t.Fatalf("first generations: %d is number %d, %d is number %d", first.ID, first.Number, other.ID, other.Number)
	}
	// The games take turns, so their generation IDs interleave.
	ids := map[uint][]uint{first.GameID: {first.ID}, other.GameID: {other.ID}}
	for _, gameID := range []uint{first.GameID, other.GameID, first.GameID} {
		id, err := services.MigrateGeneration(db, gameID, []models.CommandFields{})
		if err != nil {
			t.Fatal(err)
		}
		ids[gameID] = append(ids[gameID], id)
	}
	for gameID, gameIDs := range ids {
		for index, id := range gameIDs {
			generation, err := dal.GetGeneration(db, id)
			if err != nil || generation.Number != index+1 {
				t.Fatalf("game %d: generation %d is number %+v, want %d (%v)", gameID, id, generation, index+1, err)
			}
			refreshed := models.Generation{}
			refreshed.ID = id
			if err := dal.RefreshGeneration(db, &refreshed); err != nil || refreshed.Number != index+1 {
				t.Fatalf("game %d: refreshed generation %d is number %d, want %d (%v)", gameID, id, refreshed.Number, index+1, err)
			}
		}
		latest, err := dal.GetLatestGenerationForGame(db, gameID)
		if err != nil || latest.ID != gameIDs[len(gameIDs)-1] || latest.Number != len(gameIDs) {
			t.Fatalf("game %d: latest is %+v, want number %d (%v)", gameID, latest, len(gameIDs), err)
		}
		loaded, err := dal.LoadGameGenerations(db, gameID)
		if err != nil || len(loaded) != len(gameIDs) {
			t.Fatalf("game %d: loaded %d generations (%v)", gameID, len(loaded), err)
		}
		for index, generation := range loaded {
			if generation.ID != gameIDs[index] || generation.Number != index+1 {
				t.Fatalf("game %d: loaded generation %d is number %d, want %d", gameID, generation.ID, generation.Number, index+1)
			}
		}
	}
}

func TestUniquePersistentCastleID(t *testing.T) {
	db := testutils.InitTestDB(t)
	generationID := testutils.CreateGenerationForTest(t, db)
	generation, err := dal.GetGeneration(db, generationID)
	if err != nil {
		t.Fatal(err)
	}
	if generation.Map.Castles[0].PersistentCastleID == generation.Map.Castles[1].PersistentCastleID {
		t.Fatal("Castle IDs are not unique")
	}
}
