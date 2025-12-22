package dal_test

import (
	"islandBattles/backend/dal"
	"islandBattles/backend/internal/testutils"
	"islandBattles/backend/models"
	"testing"

	"gorm.io/gorm/clause"
)

func TestCreateGame(t *testing.T) {
	db := testutils.InitTestDB(t)

	gameID, err := dal.CreateGame(db)
	if err != nil {
		t.Fatalf("Failed to create game: %v", err)
	}

	if gameID == 0 {
		t.Fatalf("Expected non-zero gameID, got 0")
	}

	var game models.Game
	if err := db.First(&game, gameID).Error; err != nil {
		t.Fatalf("Failed to find game: %v", err)
	}
}

func TestAddPlayerToGame(t *testing.T) {
	db := testutils.InitTestDB(t)

	gameID, err := dal.CreateGame(db)
	if err != nil {
		t.Fatalf("Failed to create game: %v", err)
	}

	// Add first player
	if err := dal.AddPlayerToGame(db, gameID); err != nil {
		t.Fatalf("Failed to add first player: %v", err)
	}

	// Add second player
	if err := dal.AddPlayerToGame(db, gameID); err != nil {
		t.Fatalf("Failed to add second player: %v", err)
	}

	// Verify players
	var players []models.Player
	if err := db.Where(&models.Player{GameID: gameID}).Order(clause.OrderByColumn{Column: clause.Column{Name: "team_color"},
		Desc: false,
	}).Find(&players).
		Error; err != nil {
		t.Fatalf("Failed to find players: %v", err)
	}

	if len(players) != 2 {
		t.Fatalf("Expected 2 players, got %d", len(players))
	}

	if players[0].TeamColor != 0 {
		t.Errorf("Expected first player to have TeamColor 0, got %d", players[0].TeamColor)
	}

	if players[1].TeamColor != 1 {
		t.Errorf("Expected second player to have TeamColor 1, got %d", players[1].TeamColor)
	}

	// Delete player 0
	if err := db.Unscoped().Delete(&players[0]).Error; err != nil {
		t.Fatalf("Failed to delete player 0: %v", err)
	}

	// Add another player.
	// Current max color is 1 (player 1).
	// New player should get 1 + 1 = 2.
	if err := dal.AddPlayerToGame(db, gameID); err != nil {
		t.Fatalf("Failed to add third player: %v", err)
	}

	var newPlayers []models.Player
	if err := db.Where(&models.Player{GameID: gameID}).Find(&newPlayers).Error; err != nil {
		t.Fatalf("Failed to find players after addition: %v", err)
	}

	found2 := false
	for _, p := range newPlayers {
		if p.TeamColor == 2 {
			found2 = true
			break
		}
	}
	if !found2 {
		t.Errorf("Expected new player to have TeamColor 2")
	}
}
