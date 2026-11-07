package main

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"
)

const artifactPath = "../../api/openapi.json"

func TestCheckedInPlayerOpenAPIIsCurrent(t *testing.T) {
	document, err := BuildPlayerOpenAPI()
	if err != nil {
		t.Fatal(err)
	}
	current, err := os.ReadFile(artifactPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(current, document) {
		t.Fatal("backend/api/openapi.json is stale; run go run ./backend/cmd/export-openapi --output backend/api/openapi.json")
	}
}

func TestPlayerOpenAPIPublishesGeneratableModels(t *testing.T) {
	raw, err := BuildPlayerOpenAPI()
	if err != nil {
		t.Fatal(err)
	}
	var document struct {
		Paths      map[string]map[string]struct{ OperationID string } `json:"paths"`
		Components struct {
			Schemas map[string]struct {
				Properties map[string]struct {
					Ref           string         `json:"$ref"`
					Type          any            `json:"type"`
					Enum          []any          `json:"enum"`
					PropertyNames map[string]any `json:"propertyNames"`
				} `json:"properties"`
				OneOf []struct {
					Ref string `json:"$ref"`
				} `json:"oneOf"`
				Discriminator struct {
					PropertyName string            `json:"propertyName"`
					Mapping      map[string]string `json:"mapping"`
				} `json:"discriminator"`
				Enum []any `json:"enum"`
			} `json:"schemas"`
		} `json:"components"`
	}
	if err := json.Unmarshal(raw, &document); err != nil {
		t.Fatal(err)
	}
	operations := map[string]bool{}
	for _, methods := range document.Paths {
		for _, operation := range methods {
			operations[operation.OperationID] = true
		}
	}
	if len(operations) != len(PlayerOperations) {
		t.Fatalf("unexpected operations: %v", operations)
	}
	schemas := document.Components.Schemas
	for _, name := range []string{"Generation", "Castle", "SoldierGroup", "Clash", "ClashParticipant", "GenerationState", "GenerationEvent", "GameEndedEvent", "GameOutcome", "SendSoldierGroupParameters", "UpgradeCastleParameters", "UpgradeType", "Upgrade", "UpgradeLevel", "MapTemplate", "CastleTemplate", "Demo", "GameTeam", "GenerationLogs", "BotTurn", "BotTurnResult"} {
		if _, exists := schemas[name]; !exists {
			t.Fatalf("schema %s is missing", name)
		}
	}
	// A player command is one of the two command shapes, chosen by command_type.
	command := schemas["PublicGenerationCommand"]
	shapes := map[string]string{"send_soldier_group": "SendSoldierGroup", "upgrade_castle": "UpgradeCastle"}
	if command.Discriminator.PropertyName != "command_type" || len(command.OneOf) != len(shapes) || len(command.Discriminator.Mapping) != len(shapes) {
		t.Fatalf("player command is not a discriminated union: %+v", command)
	}
	for commandType, shape := range shapes {
		properties := schemas[shape+"Command"].Properties
		if command.Discriminator.Mapping[commandType] != schemaRefPrefix+shape+"Command" || properties["parameters"].Ref != schemaRefPrefix+shape+"Parameters" || len(properties["command_type"].Enum) != 1 || properties["command_type"].Enum[0] != commandType {
			t.Fatalf("%s is not typed: %+v", commandType, properties)
		}
	}
	if len(schemas["UpgradeType"].Enum) != 2 || schemas["UpgradeCastleParameters"].Properties["upgrade_type"].Ref != schemaRefPrefix+"UpgradeType" {
		t.Fatalf("upgrade types are not one enum: %+v", schemas["UpgradeType"])
	}
	for _, name := range []string{"Castle", "SoldierGroup"} {
		if schemas[name].Properties["upgrades"].PropertyNames["$ref"] != schemaRefPrefix+"UpgradeType" {
			t.Fatalf("%s.upgrades is not keyed by UpgradeType", name)
		}
	}
	for _, name := range []string{"Generation", "Clash", "SendSoldierGroupParameters", "UpgradeCastleParameters", "Upgrade", "SoldierGroup", "MapTemplate", "CastleTemplate"} {
		for property, schema := range schemas[name].Properties {
			if types, isList := schema.Type.([]any); isList {
				t.Fatalf("%s.%s is nullable: %v", name, property, types)
			}
		}
	}
}

// A demo is a file of its own keys, and a demo of a running game has a null
// outcome.
func TestPlayerOpenAPIPublishesTheDemoFileFormat(t *testing.T) {
	raw, err := BuildPlayerOpenAPI()
	if err != nil {
		t.Fatal(err)
	}
	var document struct {
		Components struct {
			Schemas map[string]struct {
				Properties map[string]struct {
					Ref   string `json:"$ref"`
					AnyOf []struct {
						Ref  string `json:"$ref"`
						Type string `json:"type"`
					} `json:"anyOf"`
					Items struct {
						Ref string `json:"$ref"`
					} `json:"items"`
				} `json:"properties"`
				Required []string `json:"required"`
			} `json:"schemas"`
		} `json:"components"`
	}
	if err := json.Unmarshal(raw, &document); err != nil {
		t.Fatal(err)
	}
	demo := document.Components.Schemas["Demo"]
	if len(demo.Properties) != 8 || len(demo.Required) != 8 {
		t.Fatalf("Demo has properties %v and requires %v", demo.Properties, demo.Required)
	}
	outcome := demo.Properties["outcome"].AnyOf
	if len(outcome) != 2 || outcome[0].Ref != schemaRefPrefix+"GameOutcome" || outcome[1].Type != "null" {
		t.Fatalf("Demo.outcome = %+v", outcome)
	}
	for property, item := range map[string]string{"frames": "Generation", "logs": "GenerationLogs", "teams": "GameTeam"} {
		if demo.Properties[property].Items.Ref != schemaRefPrefix+item {
			t.Fatalf("Demo.%s holds %q", property, demo.Properties[property].Items.Ref)
		}
	}
}
