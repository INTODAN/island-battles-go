package models

import (
	"encoding/json"
	"errors"
	"math"
	"reflect"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/jackc/pgx/v5/pgtype"
)

const exampleMapFile = `{
  "map_name": "crossing",
  "player_amount": 2,
  "plane_size_x": 8,
  "plane_size_y": 6,
  "neutral_starting_soldier_amount": 10,
  "castles": [
    {"team_color": 1, "x": -4, "y": 0, "starting_soldier_amount": 30, "upgrades": {"speed": 2}},
    {"team_color": 2, "x": 4, "y": 0, "starting_soldier_amount": 30},
    {"team_color": 0, "x": 0, "y": -3}
  ]
}`

func TestMapFileBuildsItsMap(t *testing.T) {
	var template MapTemplateView
	if err := json.Unmarshal([]byte(exampleMapFile), &template); err != nil {
		t.Fatal(err)
	}
	if err := ValidateMapTemplate(template); err != nil {
		t.Fatalf("castles on the plane's edges are inside it: %v", err)
	}
	point := func(x, y float64) pgtype.Point { return pgtype.Point{P: pgtype.Vec2{X: x, Y: y}, Valid: true} }
	expected := Map{
		MapFields: MapFields{MapName: "crossing", PlayerAmount: 2, PlaneSizeX: 8, PlaneSizeY: 6, NeutralStartingSoldierAmount: 10},
		Castles: []Castle{
			{CastleFields: CastleFields{TeamColor: 1, Position: point(-4, 0), PersistentCastleID: 0, Upgrades: Upgrades{SpeedUpgrade: 2}.Clone()}, SoldierAmount: 30},
			{CastleFields: CastleFields{TeamColor: 2, Position: point(4, 0), PersistentCastleID: 1, Upgrades: Upgrades{}.Clone()}, SoldierAmount: 30},
			{CastleFields: CastleFields{TeamColor: NeutralTeamColor, Position: point(0, -3), PersistentCastleID: 2, Upgrades: Upgrades{}.Clone()}, SoldierAmount: 10},
		},
	}
	if built := BuildMap(template); !reflect.DeepEqual(built, expected) {
		t.Fatalf("built map differs:\n got %+v\nwant %+v", built, expected)
	}
}

func TestStoredTemplateKeepsTheMapFile(t *testing.T) {
	var template MapTemplateView
	if err := json.Unmarshal([]byte(exampleMapFile), &template); err != nil {
		t.Fatal(err)
	}
	stored := NewMapTemplate(template)
	// Rows may load in any order; castle IDs restore the file's order.
	stored.CastleTemplates[0], stored.CastleTemplates[2] = stored.CastleTemplates[2], stored.CastleTemplates[0]
	if rebuilt := BuildMap(stored.View()); !reflect.DeepEqual(rebuilt, BuildMap(template)) {
		t.Fatalf("stored template builds another map:\n got %+v\nwant %+v", rebuilt, BuildMap(template))
	}
}

func TestValidateMapTemplateReportsEveryInvalidField(t *testing.T) {
	template := MapTemplateView{
		MapFields: MapFields{MapName: " ", PlayerAmount: 2, PlaneSizeX: 4, PlaneSizeY: 0},
		Castles: []CastleTemplateView{
			{TeamColor: 3, X: -2.5, Y: 0.5},
			{TeamColor: 1, X: 0, Y: math.NaN(), Upgrades: Upgrades{SpeedUpgrade: 5, "armour": 1}},
		},
	}
	err := ValidateMapTemplate(template)
	var invalidTemplate *MapTemplateError
	if !errors.As(err, &invalidTemplate) {
		t.Fatalf("expected a MapTemplateError, got %v", err)
	}
	locations := []string{}
	for _, detail := range invalidTemplate.Details {
		locations = append(locations, detail.Location)
	}
	expected := []string{"map_name", "plane_size_y", "castles[0].team_color", "castles[0].x", "castles[0].y", "castles[1].y", "castles[1].upgrades.armour", "castles[1].upgrades.speed"}
	if !reflect.DeepEqual(locations, expected) {
		t.Fatalf("locations=%v, want %v", locations, expected)
	}
	located := invalidTemplate.Located("body")
	var detail *huma.ErrorDetail
	if !errors.As(located[3], &detail) || detail.Location != "body.castles[0].x" || detail.Value != -2.5 {
		t.Fatalf("unexpected located detail: %v", located[3])
	}
	if invalidTemplate.Details[3].Location != "castles[0].x" {
		t.Fatal("Located changed the original details")
	}
}

func TestValidateMapTemplateNeedsCastles(t *testing.T) {
	err := ValidateMapTemplate(MapTemplateView{MapFields: MapFields{MapName: "empty", PlayerAmount: 0, PlaneSizeX: 1, PlaneSizeY: 1}})
	var invalidTemplate *MapTemplateError
	if !errors.As(err, &invalidTemplate) || len(invalidTemplate.Details) != 2 || invalidTemplate.Details[0].Location != "player_amount" || invalidTemplate.Details[1].Location != "castles" {
		t.Fatalf("expected player_amount and castles to be rejected, got %v", err)
	}
}
