package models

import (
	"errors"
	"fmt"
	"reflect"
	"testing"
)

func TestMapTemplateErrorsAreCapped(t *testing.T) {
	template := MapTemplateView{MapFields: MapFields{MapName: "far", PlayerAmount: 1, PlaneSizeX: 2, PlaneSizeY: 2}}
	for range MaxMapTemplateProblems + 50 {
		template.Castles = append(template.Castles, CastleTemplateView{X: 5})
	}
	var invalidTemplate *MapTemplateError
	if err := ValidateMapTemplate(template); !errors.As(err, &invalidTemplate) {
		t.Fatalf("err=%v", err)
	}
	details := invalidTemplate.Details
	if len(details) != MaxMapTemplateProblems+1 || details[MaxMapTemplateProblems-1].Location != fmt.Sprintf("castles[%d].x", MaxMapTemplateProblems-1) {
		t.Fatalf("%d details, the last kept at %q", len(details), details[len(details)-2].Location)
	}
	if last := details[MaxMapTemplateProblems]; last.Location != "" || last.Message != "and 50 more problems" {
		t.Fatalf("last detail=%+v", last)
	}
}

func TestDecodeMapTemplateEchoesOnlyScalarValues(t *testing.T) {
	_, err := DecodeMapTemplate([]byte(`{"map_name":"a","player_amount":1,"plane_size_x":2,"plane_size_y":2,"neutral_starting_soldier_amount":1,"castles":[{"team_color":1,"x":"0","y":0,"extra":true}]}`))
	var invalidTemplate *MapTemplateError
	if !errors.As(err, &invalidTemplate) || len(invalidTemplate.Details) != 2 {
		t.Fatalf("err=%v", err)
	}
	values := map[string]any{}
	for _, detail := range invalidTemplate.Details {
		values[detail.Location] = detail.Value
	}
	// The unknown key's value would be the whole castle; the wrong type keeps its value.
	if expected := map[string]any{"castles[0].x": "0", "castles[0].extra": nil}; !reflect.DeepEqual(values, expected) {
		t.Fatalf("values=%v, want %v", values, expected)
	}
}
