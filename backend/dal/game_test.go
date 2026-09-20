package dal_test

import (
	"islandBattles/backend/dal"
	"islandBattles/backend/internal/testutils"
	"islandBattles/backend/models"
	"testing"
)

func TestGetGamePreservesGenerationPreloads(t *testing.T) {
	db := testutils.InitTestDB(t)
	generationID := testutils.CreateGenerationForTest(t, db)
	generation, err := dal.GetGeneration(db, generationID)
	if err != nil {
		t.Fatal(err)
	}
	game, err := dal.GetGameByID(db, generation.GameID)
	if err != nil {
		t.Fatal(err)
	}
	if len(game.Generations) < 1 {
		t.Fatal("Game has no generations")
	}
	if game.Generations[0].Map.ID == 0 {
		t.Fatal("Game has no Map loaded.")
	}
	if game.Generations[0].Map.Castles == nil {
		t.Fatal("Game has no Castles loaded.")
	}
	if game.Generations[0].Commands == nil {
		t.Fatal("Game has no Commands loaded.")
	}
	if game.Generations[0].SoldierGroups == nil {
		t.Fatal("Game has no Commands loaded.")
	}
}

func TestCreateMultipleGames(t *testing.T) {
	db := testutils.InitTestDB(t)

	firstID, err := dal.CreateGame(db)
	if err != nil {
		t.Fatalf("Failed to create game: %v", err)
	}
	secondID, err := dal.CreateGame(db)
	if err != nil {
		t.Fatalf("Failed to create second game: %v", err)
	}
	if firstID == 0 || secondID == 0 || firstID == secondID {
		t.Fatalf("detached game IDs = %d, %d", firstID, secondID)
	}
	var game models.Game
	if err := db.First(&game, firstID).Error; err != nil {
		t.Fatalf("Failed to find game: %v", err)
	}
	if game.LobbyID != nil {
		t.Fatalf("fixture game lobby link = %q, want NULL", *game.LobbyID)
	}
}
