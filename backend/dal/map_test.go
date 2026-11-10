package dal_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"islandBattles/backend/dal"
	"islandBattles/backend/internal/testutils"
	"islandBattles/backend/models"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
)

func TestCreateMapTemplate(t *testing.T) {
	db := testutils.InitTestDB(t)

	var mapTemplate models.MapTemplateView
	err := json.Unmarshal([]byte(testutils.MapExampleInput), &mapTemplate)
	if err != nil {
		t.Fatalf("Failed to unmarshal MapExampleInput: %v", err)
	}

	// First creation should succeed
	id, err := dal.CreateMapTemplate(db, mapTemplate)
	if err != nil {
		t.Fatalf("Expected no error on first creation, got: %v", err)
	}
	if id == 0 {
		t.Fatal("Expected non-zero ID on first creation")
	}

	// Verify that the map template and its castle positions were saved correctly
	var savedTemplate models.MapTemplate
	if err := db.Preload("CastleTemplates").First(&savedTemplate, id).Error; err != nil {
		t.Fatalf("Failed to retrieve saved map template: %v", err)
	}

	if len(savedTemplate.CastleTemplates) != 2 {
		t.Fatalf("Expected 2 castles, got %d", len(savedTemplate.CastleTemplates))
	}

	// Check first castle position (expected (0,1) from MapExampleInput)
	expectedX, expectedY := 0.0, 1.0
	if savedTemplate.CastleTemplates[0].Position.P.X != expectedX || savedTemplate.CastleTemplates[0].Position.P.Y != expectedY {
		t.Errorf("Expected first castle position (%f, %f), got (%f, %f)",
			expectedX, expectedY,
			savedTemplate.CastleTemplates[0].Position.P.X,
			savedTemplate.CastleTemplates[0].Position.P.Y)
	}

	// Second creation should fail due to duplicate name
	_, err = dal.CreateMapTemplate(db, mapTemplate)
	if !errors.Is(err, dal.ErrMapTemplateNameTaken) {
		t.Fatalf("Expected a taken name on second creation, got %v", err)
	}
}

// map_templates has no unique index on map_name, so only the advisory lock in
// CreateMapTemplate keeps concurrent creates from storing the same name twice.
func TestCreateMapTemplateStoresANameOnceUnderConcurrentCreates(t *testing.T) {
	db := testutils.InitTestDB(t)
	const creators, rounds = 8, 10
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	// Open every connection up front so the creators race on the database
	// rather than on dialling it.
	sqlDB.SetMaxOpenConns(creators)
	sqlDB.SetMaxIdleConns(creators)
	var warm sync.WaitGroup
	for range creators {
		warm.Go(func() { _ = db.Exec("SELECT pg_sleep(0.05)").Error })
	}
	warm.Wait()

	for round := range rounds {
		template := models.MapTemplateView{
			MapFields: models.MapFields{MapName: fmt.Sprintf("contested-%d", round), PlayerAmount: 2, PlaneSizeX: 4, PlaneSizeY: 4, NeutralStartingSoldierAmount: 10},
			Castles:   []models.CastleTemplateView{{TeamColor: 1, X: -1, Y: 0}, {TeamColor: 2, X: 1, Y: 0}},
		}
		start := make(chan struct{})
		errs := make([]error, creators)
		var group sync.WaitGroup
		for creator := range creators {
			group.Go(func() {
				<-start
				_, errs[creator] = dal.CreateMapTemplate(db, template)
			})
		}
		close(start)
		group.Wait()

		created := 0
		for creator, err := range errs {
			switch {
			case err == nil:
				created++
			case !errors.Is(err, dal.ErrMapTemplateNameTaken):
				t.Fatalf("round %d: creator %d failed: %v", round, creator, err)
			}
		}
		var stored int64
		if err := db.Model(&models.MapTemplate{}).Where("map_name = ?", template.MapName).Count(&stored).Error; err != nil {
			t.Fatal(err)
		}
		if created != 1 || stored != 1 {
			t.Fatalf("round %d: %d of %d concurrent creates succeeded and %d templates named %q were stored, want 1", round, created, creators, stored, template.MapName)
		}
	}
}

func TestCreateMapTemplateStoresNothingForAnInvalidMap(t *testing.T) {
	db := testutils.InitTestDB(t)
	template := models.MapTemplateView{
		MapFields: models.MapFields{MapName: "outside", PlayerAmount: 2, PlaneSizeX: 4, PlaneSizeY: 4},
		Castles:   []models.CastleTemplateView{{TeamColor: 1, X: 3, Y: 0}, {TeamColor: 2, X: 0, Y: 0}},
	}
	_, err := dal.CreateMapTemplate(db, template)
	var invalidTemplate *models.MapTemplateError
	if !errors.As(err, &invalidTemplate) || len(invalidTemplate.Details) != 1 || invalidTemplate.Details[0].Location != "castles[0].x" {
		t.Fatalf("expected the castle's x to be rejected, got %v", err)
	}
	var stored int64
	if err := db.Model(&models.MapTemplate{}).Count(&stored).Error; err != nil {
		t.Fatal(err)
	}
	if stored != 0 {
		t.Fatalf("stored %d invalid templates", stored)
	}
}

func TestCreateMapFromTemplateStartsCastlesWithTheirOwnSoldiers(t *testing.T) {
	db := testutils.InitTestDB(t)
	thirty := uint(30)
	template := models.MapTemplateView{
		MapFields: models.MapFields{MapName: "garrisons", PlayerAmount: 2, PlaneSizeX: 8, PlaneSizeY: 6, NeutralStartingSoldierAmount: 15},
		Castles: []models.CastleTemplateView{
			{TeamColor: 1, X: -3, Y: 0, StartingSoldierAmount: &thirty, Upgrades: models.Upgrades{models.SpeedUpgrade: 2}},
			{TeamColor: 2, X: 3, Y: 0},
			{TeamColor: models.NeutralTeamColor, X: 0, Y: 2.5},
		},
	}
	templateID, err := dal.CreateMapTemplate(db, template)
	if err != nil {
		t.Fatal(err)
	}
	mapID, err := dal.CreateMapFromTemplate(db, templateID)
	if err != nil {
		t.Fatal(err)
	}
	gameMap, err := dal.GetMap(db, mapID)
	if err != nil {
		t.Fatal(err)
	}
	soldiers := map[uint]uint{}
	for _, castle := range gameMap.Castles {
		soldiers[castle.PersistentCastleID] = castle.SoldierAmount
		if castle.PersistentCastleID == 0 && castle.Upgrades[models.SpeedUpgrade] != 2 {
			t.Fatalf("castle 0 lost its upgrade: %v", castle.Upgrades)
		}
	}
	if soldiers[0] != 30 || soldiers[1] != 15 || soldiers[2] != 15 || len(soldiers) != 3 {
		t.Fatalf("unexpected starting soldiers by castle ID: %v", soldiers)
	}
}

// Templates stored before starting soldier counts and plane checks existed
// have NULL counts and may lie outside today's plane; they keep working.
func TestCreateMapFromTemplateKeepsEarlierTemplates(t *testing.T) {
	db := testutils.InitTestDB(t)
	point := func(x, y float64) pgtype.Point { return pgtype.Point{P: pgtype.Vec2{X: x, Y: y}, Valid: true} }
	earlier := models.MapTemplate{
		MapFields: models.MapFields{MapName: "earlier", PlayerAmount: 2, PlaneSizeX: 4, PlaneSizeY: 3, NeutralStartingSoldierAmount: 15},
		CastleTemplates: []models.CastleTemplate{
			{CastleFields: models.CastleFields{TeamColor: 2, Position: point(3, 1), PersistentCastleID: 1, Upgrades: models.Upgrades{}.Clone()}},
			{CastleFields: models.CastleFields{TeamColor: 1, Position: point(0, 1), PersistentCastleID: 0, Upgrades: models.Upgrades{}.Clone()}},
		},
	}
	if err := db.Create(&earlier).Error; err != nil {
		t.Fatal(err)
	}
	mapID, err := dal.CreateMapFromTemplate(db, earlier.ID)
	if err != nil {
		t.Fatal(err)
	}
	gameMap, err := dal.GetMap(db, mapID)
	if err != nil {
		t.Fatal(err)
	}
	if len(gameMap.Castles) != 2 {
		t.Fatalf("expected 2 castles, got %d", len(gameMap.Castles))
	}
	for _, castle := range gameMap.Castles {
		if castle.SoldierAmount != 15 || castle.TeamColor != castle.PersistentCastleID+1 {
			t.Fatalf("castle %d changed: color=%d soldiers=%d", castle.PersistentCastleID, castle.TeamColor, castle.SoldierAmount)
		}
	}
}
