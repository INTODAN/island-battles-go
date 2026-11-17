package main

import (
	"encoding/json"
	"os"
	"reflect"
	"slices"
	"testing"
)

// openAPIDocument is the player API, whose Demo schema is the demo format.
const openAPIDocument = "../../../../backend/api/openapi.json"

// The server reads the demo format version that the game writes, and every
// field of a demo it decodes is one the format has.
func TestTheDemoTypesFollowTheDemoSchema(t *testing.T) {
	raw, err := os.ReadFile(openAPIDocument)
	if err != nil {
		t.Fatal(err)
	}
	var document struct {
		Components struct {
			Schemas map[string]struct {
				Properties map[string]struct {
					Enum []any `json:"enum"`
				} `json:"properties"`
			} `json:"schemas"`
		} `json:"components"`
	}
	if err := json.Unmarshal(raw, &document); err != nil {
		t.Fatal(err)
	}
	schemas := document.Components.Schemas
	if versions := schemas["Demo"].Properties["schema_version"].Enum; !reflect.DeepEqual(versions, []any{float64(demoSchemaVersion)}) {
		t.Fatalf("the game writes demo format versions %v, and this server reads %d", versions, demoSchemaVersion)
	}
	for schema, value := range map[string]any{"Demo": Demo{}, "GameTeam": DemoTeam{}, "GameOutcome": DemoOutcome{}, "GenerationLogs": GenerationLogs{}, "PlayerGenerationLogs": PlayerLogs{}} {
		properties := schemas[schema].Properties
		if len(properties) == 0 {
			t.Fatalf("no %s schema", schema)
		}
		fields := reflect.TypeOf(value)
		for index := range fields.NumField() {
			name := fields.Field(index).Tag.Get("json")
			if _, found := properties[name]; !found {
				names := make([]string, 0, len(properties))
				for property := range properties {
					names = append(names, property)
				}
				slices.Sort(names)
				t.Errorf("%s has no %s; it has %v", schema, name, names)
			}
		}
	}
}
