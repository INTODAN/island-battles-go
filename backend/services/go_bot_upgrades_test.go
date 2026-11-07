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
	turn := services.NewGoBotTurn(&models.Game{}, &models.Generation{}, 1, nil)
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
