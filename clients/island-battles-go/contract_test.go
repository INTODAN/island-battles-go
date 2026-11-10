package islandbattles

import (
	"encoding/json"
	"maps"
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"
)

// openAPIDocument is the player API document the server publishes.
const openAPIDocument = "../../backend/api/openapi.json"

// schemaTypes are the SDK's types for the schemas that BotTurn and
// BotTurnResult reference. PublicGenerationCommand is a oneOf of the two
// command schemas, which Command carries both of.
var schemaTypes = map[string]reflect.Type{
	"BotTurn":                    reflect.TypeFor[Turn](),
	"Generation":                 reflect.TypeFor[Generation](),
	"Castle":                     reflect.TypeFor[Castle](),
	"SoldierGroup":               reflect.TypeFor[SoldierGroup](),
	"Clash":                      reflect.TypeFor[Clash](),
	"ClashParticipant":           reflect.TypeFor[ClashParticipant](),
	"Upgrade":                    reflect.TypeFor[Upgrade](),
	"UpgradeLevel":               reflect.TypeFor[UpgradeLevel](),
	"BotTurnResult":              reflect.TypeFor[Result](),
	"SendSoldierGroupCommand":    reflect.TypeFor[Command](),
	"UpgradeCastleCommand":       reflect.TypeFor[Command](),
	"SendSoldierGroupParameters": reflect.TypeFor[SendSoldierGroupParameters](),
	"UpgradeCastleParameters":    reflect.TypeFor[UpgradeCastleParameters](),
	"GenerationLogLine":          reflect.TypeFor[LogLine](),
}

type schema struct {
	Type       any                        `json:"type"`
	Properties map[string]json.RawMessage `json:"properties"`
	Required   []string                   `json:"required"`
	Enum       []string                   `json:"enum"`
	OneOf      []json.RawMessage          `json:"oneOf"`
}

// loadSchemas reads the document's schemas, decoded and raw.
func loadSchemas(t *testing.T) (map[string]schema, map[string]json.RawMessage) {
	t.Helper()
	data, err := os.ReadFile(openAPIDocument)
	if err != nil {
		t.Fatal(err)
	}
	var document struct {
		Components struct {
			Schemas map[string]json.RawMessage `json:"schemas"`
		} `json:"components"`
	}
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	schemas := map[string]schema{}
	for name, raw := range document.Components.Schemas {
		var decoded schema
		if err := json.Unmarshal(raw, &decoded); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		schemas[name] = decoded
	}
	return schemas, document.Components.Schemas
}

// references lists the schema names a raw schema refers to.
func references(raw json.RawMessage) []string {
	var value any
	_ = json.Unmarshal(raw, &value)
	names := []string{}
	var walk func(any)
	walk = func(value any) {
		switch value := value.(type) {
		case map[string]any:
			for key, item := range value {
				if reference, ok := item.(string); ok && key == "$ref" {
					names = append(names, strings.TrimPrefix(reference, "#/components/schemas/"))
				}
				walk(item)
			}
		case []any:
			for _, item := range value {
				walk(item)
			}
		}
	}
	walk(value)
	return names
}

// jsonNames lists a struct type's JSON field names.
func jsonNames(structType reflect.Type) []string {
	names := []string{}
	for index := range structType.NumField() {
		name, _, _ := strings.Cut(structType.Field(index).Tag.Get("json"), ",")
		if name != "" && name != "-" {
			names = append(names, name)
		}
	}
	slices.Sort(names)
	return names
}

// Every object schema that BotTurn and BotTurnResult reach has an SDK type
// with exactly its properties, and every one is required, so the SDK always
// writes it.
func TestTypesMatchTheOpenAPIDocument(t *testing.T) {
	schemas, rawSchemas := loadSchemas(t)
	seen := map[string]bool{}
	pending := []string{"BotTurn", "BotTurnResult"}
	for len(pending) > 0 {
		name := pending[0]
		pending = pending[1:]
		if seen[name] {
			continue
		}
		seen[name] = true
		pending = append(pending, references(rawSchemas[name])...)
		current, found := schemas[name]
		if !found {
			t.Fatalf("the document has no schema %s", name)
		}
		if len(current.Properties) == 0 {
			continue
		}
		goType, found := schemaTypes[name]
		if !found {
			t.Errorf("schema %s has no SDK type; add one and list it in schemaTypes", name)
			continue
		}
		properties := slices.Sorted(maps.Keys(current.Properties))
		if names := jsonNames(goType); !slices.Equal(names, properties) {
			t.Errorf("schema %s has properties %v, but %s has %v", name, properties, goType.Name(), names)
		}
		if required := slices.Sorted(slices.Values(current.Required)); !slices.Equal(required, properties) {
			t.Errorf("schema %s requires only %v of %v; the SDK writes every field", name, required, properties)
		}
	}
	for name := range schemaTypes {
		if !seen[name] {
			t.Errorf("schemaTypes lists %s, which BotTurn and BotTurnResult don't reach", name)
		}
	}
	if _, found := schemas["PublicGenerationCommand"]; !found || len(schemas["PublicGenerationCommand"].OneOf) != 2 {
		t.Error("PublicGenerationCommand is no longer one of two command schemas")
	}
}

// The enums of the contract are the SDK's constants.
func TestEnumsMatchTheOpenAPIDocument(t *testing.T) {
	schemas, _ := loadSchemas(t)
	upgradeTypes := []string{}
	for _, upgradeType := range UpgradeTypes() {
		upgradeTypes = append(upgradeTypes, string(upgradeType))
	}
	if enum := slices.Sorted(slices.Values(schemas["UpgradeType"].Enum)); !slices.Equal(enum, upgradeTypes) {
		t.Errorf("UpgradeType is %v in the document, %v in the SDK", enum, upgradeTypes)
	}
	var level struct {
		Enum []string `json:"enum"`
	}
	if err := json.Unmarshal(schemas["GenerationLogLine"].Properties["level"], &level); err != nil {
		t.Fatal(err)
	}
	levels := []string{}
	for _, logLevel := range LogLevels() {
		levels = append(levels, string(logLevel))
	}
	if !slices.Equal(level.Enum, levels) {
		t.Errorf("GenerationLogLine.level is %v in the document, %v in the SDK", level.Enum, levels)
	}
	for name, commandType := range map[string]CommandType{"SendSoldierGroupCommand": SendSoldierGroupCommand, "UpgradeCastleCommand": UpgradeCastleCommand} {
		var property struct {
			Enum []string `json:"enum"`
		}
		if err := json.Unmarshal(schemas[name].Properties["command_type"], &property); err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(property.Enum, []string{string(commandType)}) {
			t.Errorf("%s.command_type is %v in the document, %q in the SDK", name, property.Enum, commandType)
		}
	}
}

// The limits the document states are the SDK's constants.
func TestLimitsMatchTheOpenAPIDocument(t *testing.T) {
	schemas, _ := loadSchemas(t)
	var limits struct {
		Commands struct {
			MaxItems int `json:"maxItems"`
		} `json:"commands"`
		Logs struct {
			MaxItems int `json:"maxItems"`
		} `json:"logs"`
	}
	encoded, _ := json.Marshal(schemas["BotTurnResult"].Properties)
	if err := json.Unmarshal(encoded, &limits); err != nil {
		t.Fatal(err)
	}
	var line struct {
		Logger struct {
			MaxLength int `json:"maxLength"`
		} `json:"logger"`
		Message struct {
			MaxLength int `json:"maxLength"`
		} `json:"message"`
	}
	encoded, _ = json.Marshal(schemas["GenerationLogLine"].Properties)
	if err := json.Unmarshal(encoded, &line); err != nil {
		t.Fatal(err)
	}
	if limits.Commands.MaxItems != MaxCommands || limits.Logs.MaxItems != MaxLogLines || line.Logger.MaxLength != MaxLoggerLength || line.Message.MaxLength != MaxLogMessageLength {
		t.Errorf("the document's limits %+v %+v differ from the SDK's", limits, line)
	}
}
