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
					Ref  string `json:"$ref"`
					Type any    `json:"type"`
					Enum []any  `json:"enum"`
				} `json:"properties"`
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
	for _, name := range []string{"Generation", "Castle", "SoldierGroup", "Clash", "ClashParticipant", "GenerationState", "GenerationEvent", "SendSoldierGroupParameters", "MapTemplate", "CastleTemplate"} {
		if _, exists := schemas[name]; !exists {
			t.Fatalf("schema %s is missing", name)
		}
	}
	command := schemas["PublicGenerationCommand"].Properties
	if command["parameters"].Ref != schemaRefPrefix+"SendSoldierGroupParameters" || len(command["command_type"].Enum) != 1 {
		t.Fatalf("player command is not typed: %+v", command)
	}
	for _, name := range []string{"Generation", "Clash", "SendSoldierGroupParameters", "SoldierGroup", "MapTemplate", "CastleTemplate"} {
		for property, schema := range schemas[name].Properties {
			if types, isList := schema.Type.([]any); isList {
				t.Fatalf("%s.%s is nullable: %v", name, property, types)
			}
		}
	}
}
