package services_test

import (
	"testing"

	"islandBattles/backend/dal"
	"islandBattles/backend/internal/testutils"
	"islandBattles/backend/models"
	"islandBattles/backend/services"
	"islandBattles/backend/simulation"

	"encoding/json"
	"fmt"
	"github.com/jackc/pgx/v5/pgtype"
	"gorm.io/gorm"
)

func captureFixture(t *testing.T) (*gorm.DB, *models.Generation) {
	t.Helper()
	database := testutils.InitTestDB(t)
	template := models.MapTemplateView{MapFields: models.MapFields{MapName: "clash-capture", PlayerAmount: 2, PlaneSizeX: 10, PlaneSizeY: 4, NeutralStartingSoldierAmount: 100}, Castles: []models.CastleTemplateView{
		{TeamColor: 1, X: 0, Y: 1},
		{TeamColor: 2, X: 3, Y: 1},
	}}
	templateID, err := dal.CreateMapTemplate(database, template)
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
	originPersistentID, destinationPersistentID := uint(0), uint(1)
	if team == 2 {
		originPersistentID, destinationPersistentID = 1, 0
	}
	origin, err := dal.GetCastleFromGeneration(generation, originPersistentID)
	if err != nil {
		t.Fatal(err)
	}
	destination, err := dal.GetCastleFromGeneration(generation, destinationPersistentID)
	if err != nil {
		t.Fatal(err)
	}
	group := models.SoldierGroup{SoldierGroupFields: models.SoldierGroupFields{OriginCastleID: origin.ID, DestinationCastleID: destination.ID, SoldierAmount: amount}, TeamColor: team, GenerationID: generation.ID, Position: pgtype.Point{P: pgtype.Vec2{X: x, Y: 1}, Valid: true}}
	testutils.CreateSoldierGroupForTest(t, database, &group)
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
	parameters, err := json.Marshal(simulation.SendSoldierGroupParams{OriginPersistentCastleID: 1, DestinationPersistentCastleID: 0, SoldierAmount: 1})
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
	sendParameters, err := json.Marshal(simulation.SendSoldierGroupParams{OriginPersistentCastleID: 0, DestinationPersistentCastleID: 1, SoldierAmount: 10})
	if err != nil {
		t.Fatal(err)
	}
	upgradeParameters, err := json.Marshal(simulation.UpgradeCastleParams{PersistentCastleID: 0, UpgradeType: models.ClashStopGenerationsUpgrade})
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
		if group.PersistentSoldierGroupID == 1 {
			expected = 3
		}
		if group.Upgrades[models.ClashStopGenerationsUpgrade] != expected {
			t.Fatalf("wrong snapshot for group %d: %+v", group.PersistentSoldierGroupID, group.Upgrades)
		}
	}
}
