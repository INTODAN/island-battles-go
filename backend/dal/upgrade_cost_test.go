package dal_test

import (
	"errors"
	"testing"

	"islandBattles/backend/dal"
	"islandBattles/backend/internal/testutils"
	"islandBattles/backend/models"
)

func TestUpgradeCostAndAffordability(t *testing.T) {
	database := testutils.InitTestDB(t)
	generationID := testutils.CreateGenerationForTest(t, database)
	generation, err := dal.GetGeneration(database, generationID)
	if err != nil {
		t.Fatal(err)
	}
	castle, err := dal.GetCastleFromGeneration(generation, 0)
	if err != nil {
		t.Fatal(err)
	}
	for index, cost := range []uint{10, 20, 30} {
		level := index + 1
		castle.Upgrades = models.Upgrades{models.SpeedUpgrade: level}
		castle.SoldierAmount = cost - 1
		if err := database.Model(castle).Select("Upgrades", "SoldierAmount").Updates(castle).Error; err != nil {
			t.Fatal(err)
		}
		if err := dal.UpgradeCastle(database, castle, models.SpeedUpgrade); !errors.Is(err, models.ErrInsufficientSoldiers) {
			t.Fatalf("accepted unaffordable upgrade: %v", err)
		}
		if castle.SoldierAmount != cost-1 || castle.Upgrades[models.SpeedUpgrade] != level {
			t.Fatal("failed purchase mutated input")
		}
		if err := dal.RefreshGeneration(database, generation); err != nil {
			t.Fatal(err)
		}
		castle, err = dal.GetCastleFromGeneration(generation, 0)
		if err != nil {
			t.Fatal(err)
		}
		if castle.SoldierAmount != cost-1 || castle.Upgrades[models.SpeedUpgrade] != level {
			t.Fatal("failed purchase changed persisted state")
		}
		if err := dal.UpdateCastleSoldierGroupAmount(database, castle, cost); err != nil {
			t.Fatal(err)
		}
		if err := dal.UpgradeCastle(database, castle, models.SpeedUpgrade); err != nil {
			t.Fatal(err)
		}
		if err := dal.RefreshGeneration(database, generation); err != nil {
			t.Fatal(err)
		}
		castle, err = dal.GetCastleFromGeneration(generation, 0)
		if err != nil {
			t.Fatal(err)
		}
		if castle.SoldierAmount != 0 || castle.Upgrades[models.SpeedUpgrade] != level+1 {
			t.Fatalf("exact payment failed: %+v", castle)
		}
	}
}

func TestConcurrentUpgradesCannotSpendStaleBalance(t *testing.T) {
	database := testutils.InitTestDB(t)
	generationID := testutils.CreateGenerationForTest(t, database)
	generation, err := dal.GetGeneration(database, generationID)
	if err != nil {
		t.Fatal(err)
	}
	castle, err := dal.GetCastleFromGeneration(generation, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := dal.UpdateCastleSoldierGroupAmount(database, castle, 10); err != nil {
		t.Fatal(err)
	}
	results := make(chan error, 2)
	start := make(chan struct{})
	for range 2 {
		snapshot := *castle
		go func() { <-start; results <- dal.UpgradeCastle(database, &snapshot, models.SpeedUpgrade) }()
	}
	close(start)
	succeeded, rejected := 0, 0
	for range 2 {
		err := <-results
		if err == nil {
			succeeded++
		} else if errors.Is(err, models.ErrInsufficientSoldiers) {
			rejected++
		} else {
			t.Fatal(err)
		}
	}
	if succeeded != 1 || rejected != 1 {
		t.Fatalf("success=%d rejection=%d", succeeded, rejected)
	}
	if err := dal.RefreshGeneration(database, generation); err != nil {
		t.Fatal(err)
	}
	castle, err = dal.GetCastleFromGeneration(generation, 0)
	if err != nil {
		t.Fatal(err)
	}
	if castle.SoldierAmount != 0 || castle.Upgrades[models.SpeedUpgrade] != 2 {
		t.Fatalf("concurrent purchase lost a deduction: %+v", castle)
	}
}
