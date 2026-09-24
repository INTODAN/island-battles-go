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

	"github.com/jackc/pgx/v5/pgtype"
)

func TestCastleUpgradesAffectOnlyNewSoldierGroups(t *testing.T) {
	database := testutils.InitTestDB(t)
	template := models.MapTemplate{
		MapFields: models.MapFields{MapName: "upgrade-snapshots", PlayerAmount: 2, PlaneSizeX: 24, PlaneSizeY: 4, NeutralStartingSoldierAmount: 100},
		CastleTemplates: []models.CastleTemplate{
			{CastleFields: models.CastleFields{TeamColor: 1, Position: pgtype.Point{P: pgtype.Vec2{X: 0, Y: 1}, Valid: true}}},
			{CastleFields: models.CastleFields{TeamColor: 2, Position: pgtype.Point{P: pgtype.Vec2{X: 20, Y: 1}, Valid: true}}},
		},
	}
	templateID, err := dal.CreateMapTemplate(database, &template)
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
	send := command(models.SendSoldierGroup, commandHandlers.SendSoldierGroupParams{OriginLogicalCastleID: 0, DestinationLogicalCastleID: 1, SoldierAmount: 10})
	upgrade := command(models.UpgradeCastle, commandHandlers.UpgradeCastleParams{LogicalCastleID: 0, UpgradeType: models.SpeedUpgrade})
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
	if len(after.SoldierGroups) != 2 {
		t.Fatal("expected old and newly spawned groups")
	}
	for _, group := range after.SoldierGroups {
		expectedLevel, expectedX := 1, 1.0
		if group.LogicalSoldierGroupID == 1 {
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
		if group.LogicalSoldierGroupID == 1 {
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
		{command(models.UpgradeCastle, commandHandlers.UpgradeCastleParams{LogicalCastleID: 0, UpgradeType: "unknown"}), models.ErrInvalidUpgrade},
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
