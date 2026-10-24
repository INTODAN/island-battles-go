package services

import (
	"encoding/json"
	"strings"
	"testing"

	"islandBattles/backend/models"

	"github.com/jackc/pgx/v5/pgtype"
)

func TestGenerationViewSortsByPersistentID(t *testing.T) {
	castle := func(persistentID uint) models.Castle {
		return models.Castle{CastleFields: models.CastleFields{PersistentCastleID: persistentID, TeamColor: 1, Position: pgtype.Point{P: pgtype.Vec2{X: float64(persistentID)}, Valid: true}}, SoldierAmount: 10 + persistentID}
	}
	group := func(persistentID uint, origin, destination models.Castle) models.SoldierGroup {
		return models.SoldierGroup{PersistentSoldierGroupID: persistentID, TeamColor: 2, SoldierGroupFields: models.SoldierGroupFields{SoldierAmount: persistentID}, OriginCastle: origin, DestinationCastle: destination}
	}
	generation := &models.Generation{
		Map:           models.Map{Castles: []models.Castle{castle(2), castle(0), castle(1)}},
		SoldierGroups: []models.SoldierGroup{group(9, castle(2), castle(0)), group(4, castle(0), castle(1))},
		Clashes:       []models.Clash{{ID: 1}},
	}
	generation.ID = 7
	view := generationView(generation)
	if view.ID != 7 || len(view.Clashes) != 1 {
		t.Fatalf("view = %+v", view)
	}
	for index, castle := range view.Castles {
		if castle.ID != uint(index) || castle.Soldiers != 10+uint(index) || castle.X != float64(index) || castle.TeamColor != 1 {
			t.Fatalf("castles = %+v", view.Castles)
		}
	}
	first, second := view.SoldierGroups[0], view.SoldierGroups[1]
	if first.ID != 4 || second.ID != 9 || first.OriginCastleID != 0 || first.DestinationCastleID != 1 || second.OriginCastleID != 2 || first.ClashRadius != models.DefaultClashRadius {
		t.Fatalf("soldier groups = %+v", view.SoldierGroups)
	}
}

func TestGenerationViewListsAreNeverNull(t *testing.T) {
	raw, err := json.Marshal(generationView(&models.Generation{}))
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{`"castles":[]`, `"soldier_groups":[]`, `"clashes":[]`} {
		if !strings.Contains(string(raw), field) {
			t.Fatalf("view %s lacks %s", raw, field)
		}
	}
}
