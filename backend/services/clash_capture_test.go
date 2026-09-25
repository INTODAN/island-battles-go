package services_test

import (
	"testing"

	"islandBattles/backend/dal"
	"islandBattles/backend/dal/commandHandlers"
	"islandBattles/backend/dal/commandHandlers/move_soldier_groups"
	"islandBattles/backend/internal/testutils"
	"islandBattles/backend/models"
	"islandBattles/backend/services"

	"encoding/json"
	"fmt"
	"github.com/jackc/pgx/v5/pgtype"
	"gorm.io/gorm"
)

func captureFixture(t *testing.T) (*gorm.DB, *models.Generation) {
	t.Helper()
	database := testutils.InitTestDB(t)
	template := models.MapTemplate{MapFields: models.MapFields{MapName: "clash-capture", PlayerAmount: 2, PlaneSizeX: 10, PlaneSizeY: 4, NeutralStartingSoldierAmount: 100}, CastleTemplates: []models.CastleTemplate{
		{CastleFields: models.CastleFields{TeamColor: 1, Position: pgtype.Point{P: pgtype.Vec2{X: 0, Y: 1}, Valid: true}}},
		{CastleFields: models.CastleFields{TeamColor: 2, Position: pgtype.Point{P: pgtype.Vec2{X: 3, Y: 1}, Valid: true}}},
	}}
	templateID, err := dal.CreateMapTemplate(database, &template)
	if err != nil {
		t.Fatal(err)
	}
	gameID, err := dal.CreateGame(database)
	if err != nil {
		t.Fatal(err)
	}
	generationID, err := dal.CreateGenerationFromTemplate(database, templateID, gameID)
	if err != nil {
		t.Fatal(err)
	}
	generation, err := dal.GetGeneration(database, generationID)
	if err != nil {
		t.Fatal(err)
	}
	return database, generation
}

func captureGroup(t *testing.T, database *gorm.DB, generation *models.Generation, team, amount uint, x float64) {
	t.Helper()
	originLogicalID, destinationLogicalID := uint(0), uint(1)
	if team == 2 {
		originLogicalID, destinationLogicalID = 1, 0
	}
	origin, err := dal.GetCastleFromGeneration(generation, originLogicalID)
	if err != nil {
		t.Fatal(err)
	}
	destination, err := dal.GetCastleFromGeneration(generation, destinationLogicalID)
	if err != nil {
		t.Fatal(err)
	}
	group := models.SoldierGroup{SoldierGroupFields: models.SoldierGroupFields{OriginCastleID: origin.ID, DestinationCastleID: destination.ID, SoldierAmount: amount}, TeamColor: team, GenerationID: generation.ID, Position: pgtype.Point{P: pgtype.Vec2{X: x, Y: 1}, Valid: true}}
	if _, err := dal.CreateSoldierGroup(database, &group); err != nil {
		t.Fatal(err)
	}
	if err := dal.RefreshGeneration(database, generation); err != nil {
		t.Fatal(err)
	}
}

func TestOneSoldierPerTurnCannotPinCapturingArmy(t *testing.T) {
	database, generation := captureFixture(t)
	defender, err := dal.GetCastleFromGeneration(generation, 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := dal.UpdateCastleSoldierGroupAmount(database, defender, 10); err != nil {
		t.Fatal(err)
	}
	captureGroup(t, database, generation, 1, 100, 1)
	parameters, err := json.Marshal(commandHandlers.SendSoldierGroupParams{OriginLogicalCastleID: 1, DestinationLogicalCastleID: 0, SoldierAmount: 1})
	if err != nil {
		t.Fatal(err)
	}
	for turn := 1; turn <= 4; turn++ {
		generationID, err := services.MigrateGeneration(database, generation.GameID, []models.CommandFields{{CommandType: models.SendSoldierGroup, Parameters: parameters}})
		if err != nil {
			t.Fatal(err)
		}
		generation, err = dal.GetGeneration(database, generationID)
		if err != nil {
			t.Fatal(err)
		}
		defender, err = dal.GetCastleFromGeneration(generation, 1)
		if err != nil {
			t.Fatal(err)
		}
		if turn < 4 {
			found := false
			for _, group := range generation.SoldierGroups {
				if group.TeamColor == 1 {
					found = true
					if group.Position.P.X != 1+float64(turn)*.5 {
						t.Fatalf("army stalled on turn %d at %v", turn, group.Position)
					}
				}
			}
			if !found {
				t.Fatal("attacking army disappeared")
			}
		}
	}
	if defender.TeamColor != 1 || defender.SoldierAmount != 86 {
		t.Fatalf("one-soldier defense prevented correct capture: %+v", defender)
	}
	if len(generation.SoldierGroups) != 0 {
		t.Fatalf("captured troops remained in field: %+v", generation.SoldierGroups)
	}
	_ = testutils.CreateGameGraphForTest(t, database, generation.GameID)
}

func TestArrivedArmyDoesNotClashWithNewSpawn(t *testing.T) {
	database, generation := captureFixture(t)
	captureGroup(t, database, generation, 1, 120, 3)
	if err := commandHandlers.SendSoldierGroupHandler(database, generation, commandHandlers.SendSoldierGroupParams{OriginLogicalCastleID: 1, DestinationLogicalCastleID: 0, SoldierAmount: 1}); err != nil {
		t.Fatal(err)
	}
	if err := dal.RefreshGeneration(database, generation); err != nil {
		t.Fatal(err)
	}
	if err := move_soldier_groups.MoveSoldierGroupsHandler(database, generation, move_soldier_groups.MoveSoldierGroupsParams{}); err != nil {
		t.Fatal(err)
	}
	if err := dal.RefreshGeneration(database, generation); err != nil {
		t.Fatal(err)
	}
	if err := commandHandlers.RemoveArrivedSoldierGroupsHandler(database, generation, commandHandlers.RemoveArrivedSoldierGroupsParams{}); err != nil {
		t.Fatal(err)
	}
	defender, err := dal.GetCastleFromGeneration(generation, 1)
	if err != nil {
		t.Fatal(err)
	}
	if defender.TeamColor != 1 || defender.SoldierAmount != 21 {
		t.Fatalf("arrival incorrectly clashed with spawn: %+v", defender)
	}
	if len(generation.SoldierGroups) != 1 || generation.SoldierGroups[0].TeamColor != 2 || generation.SoldierGroups[0].SoldierAmount != 1 {
		t.Fatal("new spawn should remain a separate traveling group")
	}
	if len(generation.Clashes) != 0 {
		t.Fatal("arrival incorrectly emitted a clash event")
	}
}

func TestMultipleArrivalsUseUpdatedCastle(t *testing.T) {
	database, generation := captureFixture(t)
	captureGroup(t, database, generation, 1, 60, 3)
	captureGroup(t, database, generation, 1, 60, 3)
	captureGroup(t, database, generation, 1, 10, 3)
	if err := commandHandlers.RemoveArrivedSoldierGroupsHandler(database, generation, commandHandlers.RemoveArrivedSoldierGroupsParams{}); err != nil {
		t.Fatal(err)
	}
	defender, err := dal.GetCastleFromGeneration(generation, 1)
	if err != nil {
		t.Fatal(err)
	}
	if defender.TeamColor != 1 || defender.SoldierAmount != 30 || len(generation.SoldierGroups) != 0 {
		t.Fatalf("arrival damage/capture was overwritten: %+v", defender)
	}
}

func TestSurvivorCannotBypassSecondDefender(t *testing.T) {
	database, generation := captureFixture(t)
	captureGroup(t, database, generation, 1, 10, 1)
	captureGroup(t, database, generation, 2, 1, 1.25)
	captureGroup(t, database, generation, 2, 20, 1.75)
	if err := move_soldier_groups.MoveSoldierGroupsHandler(database, generation, move_soldier_groups.MoveSoldierGroupsParams{}); err != nil {
		t.Fatal(err)
	}
	if err := dal.RefreshGeneration(database, generation); err != nil {
		t.Fatal(err)
	}
	if len(generation.SoldierGroups) != 1 || generation.SoldierGroups[0].TeamColor != 2 || generation.SoldierGroups[0].SoldierAmount != 11 || generation.SoldierGroups[0].Position.P.X != 1.25 {
		t.Fatalf("later combat was bypassed or survivor stopped: %+v", generation.SoldierGroups)
	}
}

func TestEmptyAndSelfSendOrdersAreRejected(t *testing.T) {
	database, generation := captureFixture(t)
	for _, parameters := range []commandHandlers.SendSoldierGroupParams{{OriginLogicalCastleID: 0, DestinationLogicalCastleID: 1, SoldierAmount: 0}, {OriginLogicalCastleID: 0, DestinationLogicalCastleID: 0, SoldierAmount: 1}} {
		if err := commandHandlers.SendSoldierGroupHandler(database, generation, parameters); err == nil {
			t.Fatalf("invalid send accepted: %+v", parameters)
		}
	}
	if err := dal.RefreshGeneration(database, generation); err != nil {
		t.Fatal(err)
	}
	origin, err := dal.GetCastleFromGeneration(generation, 0)
	if err != nil {
		t.Fatal(err)
	}
	if origin.SoldierAmount != 100 || len(generation.SoldierGroups) != 0 {
		t.Fatal("rejected send changed game state")
	}
}

func TestClashPausePersistsForUpgradedGenerations(t *testing.T) {
	for level, duration := range []uint{3, 2, 1} {
		t.Run(fmt.Sprintf("duration-%d", duration), func(t *testing.T) {
			database, generation := captureFixture(t)
			captureGroup(t, database, generation, 1, 10, 1)
			captureGroup(t, database, generation, 2, 5, 1.75)
			group := &generation.SoldierGroups[0]
			group.Upgrades[models.ClashStopGenerationsUpgrade] = level + 1
			if err := database.Save(group).Error; err != nil {
				t.Fatal(err)
			}
			for turn := uint(0); turn <= duration; turn++ {
				// A small attack during the pause must neither grant movement nor reset it.
				if duration == 3 && turn == 1 {
					captureGroup(t, database, generation, 2, 1, 1.5)
				}
				generationID, err := services.MigrateGeneration(database, generation.GameID, nil)
				if err != nil {
					t.Fatal(err)
				}
				generation, err = dal.GetGeneration(database, generationID)
				if err != nil {
					t.Fatal(err)
				}
				if len(generation.SoldierGroups) != 1 {
					t.Fatalf("expected one survivor, got %+v", generation.SoldierGroups)
				}
				group := generation.SoldierGroups[0]
				expectedPosition, expectedPause := 1.375, uint(0)
				if turn < duration {
					expectedPause = duration - turn - 1
				} else {
					expectedPosition += .5
				}
				expectedSoldiers := uint(5)
				if duration == 3 && turn >= 1 {
					expectedSoldiers = 4
				}
				if group.Position.P.X != expectedPosition || group.MovementPauseGenerations != expectedPause || group.SoldierAmount != expectedSoldiers {
					t.Fatalf("turn %d: position=%v pause=%d soldiers=%d; want %v,%d,%d", turn, group.Position.P.X, group.MovementPauseGenerations, group.SoldierAmount, expectedPosition, expectedPause, expectedSoldiers)
				}
			}
		})
	}
}

func TestPausedGroupRetainsUpgradesAcrossGenerations(t *testing.T) {
	database, generation := captureFixture(t)
	captureGroup(t, database, generation, 1, 10, .5)
	group := &generation.SoldierGroups[0]
	group.Upgrades = models.Upgrades{models.SpeedUpgrade: 2}
	group.MovementPauseGenerations = 1
	if err := database.Save(group).Error; err != nil {
		t.Fatal(err)
	}
	for _, expectedPosition := range []float64{.5, 1.5} {
		generationID, err := services.MigrateGeneration(database, generation.GameID, nil)
		if err != nil {
			t.Fatal(err)
		}
		generation, err = dal.GetGeneration(database, generationID)
		if err != nil {
			t.Fatal(err)
		}
		if len(generation.SoldierGroups) != 1 {
			t.Fatal("expected the traveling group to remain")
		}
		group = &generation.SoldierGroups[0]
		if group.Position.P.X != expectedPosition || group.Upgrades[models.SpeedUpgrade] != 2 || group.MovementPauseGenerations != 0 {
			t.Fatalf("upgrade or pause lost across generations: position=%v level=%d pause=%d", group.Position.P.X, group.Upgrades[models.SpeedUpgrade], group.MovementPauseGenerations)
		}
	}
}

func TestClashStopUpgradeOnlyAffectsNewTroops(t *testing.T) {
	database, generation := captureFixture(t)
	sendParameters, err := json.Marshal(commandHandlers.SendSoldierGroupParams{OriginLogicalCastleID: 0, DestinationLogicalCastleID: 1, SoldierAmount: 10})
	if err != nil {
		t.Fatal(err)
	}
	upgradeParameters, err := json.Marshal(commandHandlers.UpgradeCastleParams{LogicalCastleID: 0, UpgradeType: models.ClashStopGenerationsUpgrade})
	if err != nil {
		t.Fatal(err)
	}
	send := models.CommandFields{CommandType: models.SendSoldierGroup, Parameters: sendParameters}
	upgrade := models.CommandFields{CommandType: models.UpgradeCastle, Parameters: upgradeParameters}
	if _, err := services.MigrateGeneration(database, generation.GameID, []models.CommandFields{send}); err != nil {
		t.Fatal(err)
	}
	generationID, err := services.MigrateGeneration(database, generation.GameID, []models.CommandFields{upgrade, upgrade, send})
	if err != nil {
		t.Fatal(err)
	}
	generation, err = dal.GetGeneration(database, generationID)
	if err != nil {
		t.Fatal(err)
	}
	castle, err := dal.GetCastleFromGeneration(generation, 0)
	if err != nil {
		t.Fatal(err)
	}
	if castle.Upgrades[models.ClashStopGenerationsUpgrade] != 3 || castle.SoldierAmount != 52 {
		t.Fatalf("upgrade purchase wrong: level=%d balance=%d", castle.Upgrades[models.ClashStopGenerationsUpgrade], castle.SoldierAmount)
	}
	if len(generation.SoldierGroups) != 2 {
		t.Fatal("expected both old and new troops")
	}
	for _, group := range generation.SoldierGroups {
		expected := 1
		if group.LogicalSoldierGroupID == 1 {
			expected = 3
		}
		if group.Upgrades[models.ClashStopGenerationsUpgrade] != expected {
			t.Fatalf("wrong snapshot for group %d: %+v", group.LogicalSoldierGroupID, group.Upgrades)
		}
	}
}
