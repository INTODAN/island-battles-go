package services_test

import (
	"encoding/json"
	"errors"
	"testing"

	"islandBattles/backend/dal"
	"islandBattles/backend/dal/commandHandlers"
	"islandBattles/backend/internal/testutils"
	"islandBattles/backend/models"
	"islandBattles/backend/services"
)

func TestCastleUpgradesAffectOnlyNewSoldierGroups(t *testing.T) {
	database := testutils.InitTestDB(t)
	template := models.MapTemplateView{
		MapFields: models.MapFields{MapName: "upgrade-snapshots", PlayerAmount: 2, PlaneSizeX: 40, PlaneSizeY: 4, NeutralStartingSoldierAmount: 100},
		Castles: []models.CastleTemplateView{
			{TeamColor: 1, X: 0, Y: 1},
			{TeamColor: 2, X: 20, Y: 1},
		},
	}
	templateID, err := dal.CreateMapTemplate(database, template)
	if err != nil {
		t.Fatal(err)
	}
	gameID, err := dal.CreateGame(database)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := dal.CreateGenerationFromTemplate(database, templateID, gameID); err != nil {
		t.Fatal(err)
	}
	command := func(commandType models.CommandType, parameters any) models.CommandFields {
		raw, err := json.Marshal(parameters)
		if err != nil {
			t.Fatal(err)
		}
		return models.CommandFields{CommandType: commandType, Parameters: raw}
	}
	send := command(models.SendSoldierGroup, commandHandlers.SendSoldierGroupParams{OriginPersistentCastleID: 0, DestinationPersistentCastleID: 1, SoldierAmount: 10})
	upgrade := command(models.UpgradeCastle, commandHandlers.UpgradeCastleParams{PersistentCastleID: 0, UpgradeType: models.SpeedUpgrade})
	beforeID, err := services.MigrateGeneration(database, gameID, []models.CommandFields{send})
	if err != nil {
		t.Fatal(err)
	}
	afterID, err := services.MigrateGeneration(database, gameID, []models.CommandFields{upgrade, upgrade, upgrade, send})
	if err != nil {
		t.Fatal(err)
	}
	before, err := dal.GetGeneration(database, beforeID)
	if err != nil {
		t.Fatal(err)
	}
	after, err := dal.GetGeneration(database, afterID)
	if err != nil {
		t.Fatal(err)
	}
	oldCastle, err := dal.GetCastleFromGeneration(before, 0)
	if err != nil {
		t.Fatal(err)
	}
	newCastle, err := dal.GetCastleFromGeneration(after, 0)
	if err != nil {
		t.Fatal(err)
	}
	if oldCastle.Upgrades[models.SpeedUpgrade] != 1 || newCastle.Upgrades[models.SpeedUpgrade] != 4 {
		t.Fatal("castle upgrades were not isolated between generations")
	}
	if oldCastle.SoldierAmount != 91 || newCastle.SoldierAmount != 22 {
		t.Fatalf("upgrade costs were not deducted: before=%d after=%d", oldCastle.SoldierAmount, newCastle.SoldierAmount)
	}
	if len(after.SoldierGroups) != 2 {
		t.Fatal("expected old and newly spawned groups")
	}
	for _, group := range after.SoldierGroups {
		expectedLevel, expectedX := 1, 1.0
		if group.PersistentSoldierGroupID == 1 {
			expectedLevel, expectedX = 4, 2
		}
		if group.Upgrades[models.SpeedUpgrade] != expectedLevel || group.Position.P.X != expectedX {
			t.Fatalf("wrong snapshot or movement: %+v", group)
		}
	}
	nextID, err := services.MigrateGeneration(database, gameID, nil)
	if err != nil {
		t.Fatal(err)
	}
	next, err := dal.GetGeneration(database, nextID)
	if err != nil {
		t.Fatal(err)
	}
	for _, group := range next.SoldierGroups {
		expectedLevel, expectedX := 1, 1.5
		if group.PersistentSoldierGroupID == 1 {
			expectedLevel, expectedX = 4, 4
		}
		if group.Upgrades[models.SpeedUpgrade] != expectedLevel || group.Position.P.X != expectedX {
			t.Fatalf("group changed upgrades in later generation: %+v", group)
		}
	}
	for _, rejected := range []struct {
		command  models.CommandFields
		expected error
	}{
		{upgrade, models.ErrUpgradeMaxLevel},
		{command(models.UpgradeCastle, commandHandlers.UpgradeCastleParams{PersistentCastleID: 0, UpgradeType: "unknown"}), models.ErrInvalidUpgrade},
	} {
		var beforeCount, afterCount int64
		if err := database.Model(&models.Generation{}).Count(&beforeCount).Error; err != nil {
			t.Fatal(err)
		}
		// A preceding send must also roll back when the upgrade is rejected.
		if generationID, err := services.MigrateGeneration(database, gameID, []models.CommandFields{send, rejected.command}); generationID != 0 || !errors.Is(err, rejected.expected) {
			t.Fatalf("unexpected rejection: generation=%d err=%v", generationID, err)
		}
		if err := database.Model(&models.Generation{}).Count(&afterCount).Error; err != nil {
			t.Fatal(err)
		}
		if beforeCount != afterCount {
			t.Fatal("failed upgrade left a partial generation")
		}
		latest, err := dal.GetLatestGenerationForGame(database, gameID)
		if err != nil {
			t.Fatal(err)
		}
		if latest.ID != nextID || len(latest.SoldierGroups) != 2 {
			t.Fatal("failed turn changed the current game")
		}
	}
	_ = testutils.CreateGameGraphForTest(t, database, gameID)
}

func TestUnaffordableUpgradeRollsBackEarlierCommands(t *testing.T) {
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
	if err := dal.UpdateCastleSoldierGroupAmount(database, castle, 40); err != nil {
		t.Fatal(err)
	}
	upgradeParameters, err := json.Marshal(commandHandlers.UpgradeCastleParams{PersistentCastleID: 0, UpgradeType: models.SpeedUpgrade})
	if err != nil {
		t.Fatal(err)
	}
	sendParameters, err := json.Marshal(commandHandlers.SendSoldierGroupParams{OriginPersistentCastleID: 0, DestinationPersistentCastleID: 1, SoldierAmount: 15})
	if err != nil {
		t.Fatal(err)
	}
	upgrade := models.CommandFields{CommandType: models.UpgradeCastle, Parameters: upgradeParameters}
	// 40 - send(15) - level2(10) leaves 15, below level3's cost of 20.
	commands := []models.CommandFields{{CommandType: models.SendSoldierGroup, Parameters: sendParameters}, upgrade, upgrade}
	if next, err := services.MigrateGeneration(database, generation.GameID, commands); next != 0 || !errors.Is(err, models.ErrInsufficientSoldiers) {
		t.Fatalf("unexpected turn result: id=%d err=%v", next, err)
	}
	generation, err = dal.GetLatestGenerationForGame(database, generation.GameID)
	if err != nil {
		t.Fatal(err)
	}
	castle, err = dal.GetCastleFromGeneration(generation, 0)
	if err != nil {
		t.Fatal(err)
	}
	if generation.ID != generationID || len(generation.SoldierGroups) != 0 || castle.SoldierAmount != 40 || castle.Upgrades[models.SpeedUpgrade] != 1 {
		t.Fatalf("failed turn did not roll back: castle=%+v generation=%+v", castle, generation)
	}
}
