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

func TestDecodeMapTemplateReadsAMapFile(t *testing.T) {
	var expected MapTemplateView
	if err := json.Unmarshal([]byte(exampleMapFile), &expected); err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeMapTemplate([]byte(exampleMapFile))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(decoded, expected) {
		t.Fatalf("decoded %+v, want %+v", decoded, expected)
	}
}

// encoding/json reads a missing field as zero and ignores unknown keys;
// DecodeMapTemplate rejects both, like the POST /map/create body schema.
func TestDecodeMapTemplateRejectsWhatEncodingJSONAccepts(t *testing.T) {
	missingRequired := func(property string) string {
		return "expected required property " + property + " to be present"
	}
	tests := []struct {
		name     string
		file     string
		location string
		message  string
	}{
		{"missing neutral soldiers", `{"map_name":"a","player_amount":2,"plane_size_x":4,"plane_size_y":4,"castles":[{"team_color":1,"x":0,"y":0}]}`, "", missingRequired("neutral_starting_soldier_amount")},
		{"missing x", `{"map_name":"a","player_amount":2,"plane_size_x":4,"plane_size_y":4,"neutral_starting_soldier_amount":1,"castles":[{"team_color":1,"y":0}]}`, "castles[0]", missingRequired("x")},
		{"missing y", `{"map_name":"a","player_amount":2,"plane_size_x":4,"plane_size_y":4,"neutral_starting_soldier_amount":1,"castles":[{"team_color":1,"x":0}]}`, "castles[0]", missingRequired("y")},
		{"missing team colour", `{"map_name":"a","player_amount":2,"plane_size_x":4,"plane_size_y":4,"neutral_starting_soldier_amount":1,"castles":[{"x":0,"y":0}]}`, "castles[0]", missingRequired("team_color")},
		{"unknown map key", `{"map_name":"a","player_amount":2,"plane_size_x":4,"plane_size_y":4,"neutral_starting_soldier_amount":1,"castles":[{"team_color":1,"x":0,"y":0}],"plane_size_z":4}`, "plane_size_z", "unexpected property"},
		{"unknown castle key", `{"map_name":"a","player_amount":2,"plane_size_x":4,"plane_size_y":4,"neutral_starting_soldier_amount":1,"castles":[{"team_color":1,"x":0,"y":0,"position":"(0,0)"}]}`, "castles[0].position", "unexpected property"},
		{"wrong type", `{"map_name":"a","player_amount":2,"plane_size_x":4,"plane_size_y":4,"neutral_starting_soldier_amount":1,"castles":[{"team_color":1,"x":"0","y":0}]}`, "castles[0].x", "expected number"},
		{"outside the plane", `{"map_name":"a","player_amount":2,"plane_size_x":4,"plane_size_y":4,"neutral_starting_soldier_amount":1,"castles":[{"team_color":1,"x":3,"y":0}]}`, "castles[0].x", "must be inside the plane, from -2 to 2"},
		{"malformed", `{"map_name":"a",`, "", "must be valid JSON: unexpected end of JSON input"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := DecodeMapTemplate([]byte(test.file))
			var invalidTemplate *MapTemplateError
			if !errors.As(err, &invalidTemplate) {
				t.Fatalf("expected a MapTemplateError, got %v", err)
			}
			if len(invalidTemplate.Details) != 1 || invalidTemplate.Details[0].Location != test.location || invalidTemplate.Details[0].Message != test.message {
				t.Fatalf("got %v, want %q at %q", err, test.message, test.location)
			}
		})
	}
}
