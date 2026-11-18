package services_test

import (
	"context"
	"encoding/json"
	"testing"

	"islandBattles/backend/handlers"
	"islandBattles/backend/models"
	"islandBattles/backend/services"
)

// A Go bot's turn carries the table GET /game/upgrades returns.
func TestGoBotTurnCarriesTheUpgradeTable(t *testing.T) {
	response, err := handlers.ListUpgrades(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	turn, err := services.NewGoBotTurn(&models.Game{}, &models.Generation{Number: 1}, 1, nil)
	if err != nil {
		t.Fatal(err)
	}
	want, err := json.Marshal(response.Body.Upgrades)
	if err != nil {
		t.Fatal(err)
	}
	got, err := json.Marshal(turn.Upgrades)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) || len(turn.Upgrades) == 0 {
		t.Fatalf("turn upgrades = %s, want %s", got, want)
	}
}

// A generation that no DAL loader numbered fails, instead of giving the bot
// turn number 0.
func TestGoBotTurnNeedsANumberedGeneration(t *testing.T) {
	if _, err := services.NewGoBotTurn(&models.Game{}, &models.Generation{}, 1, nil); err == nil {
		t.Fatal("built a turn of a generation without a number")
	}
	// The turn carries the game's own limit.
	turn, err := services.NewGoBotTurn(&models.Game{MaxGenerations: 50}, &models.Generation{Number: 7}, 1, nil)
	if err != nil || turn.TurnNumber != 7 || turn.MaxGenerations != 50 {
		t.Fatalf("turn %d of %d (%v)", turn.TurnNumber, turn.MaxGenerations, err)
	}
}
