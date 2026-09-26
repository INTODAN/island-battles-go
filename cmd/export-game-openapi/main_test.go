package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExportDeterministic(t *testing.T) {
	first, err := buildArtifacts()
	if err != nil {
		t.Fatal(err)
	}
	second, err := buildArtifacts()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first.openapi, second.openapi) || !bytes.Equal(first.events, second.events) {
		t.Fatal("independent exports differ")
	}
	if !bytes.HasSuffix(first.openapi, []byte("\n")) || !bytes.HasSuffix(first.events, []byte("\n")) {
		t.Fatal("artifacts must have one trailing newline")
	}
}

func TestExportPublicOperations(t *testing.T) {
	result, err := buildArtifacts()
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]any
	decodeJSON(t, result.openapi, &document)
	paths := document["paths"].(map[string]any)
	want := map[string]string{
		"/game/from-lobby/{lobby_id}/generation": "getIslandLobbyGeneration",
		"/generation/commands":                   "registerGenerationCommand",
		"/lobby/v1/connection-token":             "createLobbyConnectionToken",
	}

	if len(paths) != len(want) {
		t.Fatalf("path count = %d, want %d", len(paths), len(want))
	}
	for path, operationID := range want {
		pathItem, ok := paths[path].(map[string]any)
		if !ok {
			t.Fatalf("missing path %s", path)
		}
		found := false
		for _, method := range []string{"get", "post", "put", "delete"} {
			operation, ok := pathItem[method].(map[string]any)
			if !ok {
				continue
			}
			if operation["operationId"] == operationID {
				found = true
			}
		}
		if !found {
			t.Fatalf("operation %q missing at %s", operationID, path)
		}
	}
}

func TestExportExcludesInternalAndLobbyManagement(t *testing.T) {
	result, err := buildArtifacts()
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]any
	decodeJSON(t, result.openapi, &document)
	for path := range document["paths"].(map[string]any) {
		if strings.Contains(path, "/internal/") || strings.Contains(path, "replay") || strings.Contains(path, "map") || strings.Contains(path, "memberships") || strings.Contains(path, "sessions") {
			t.Fatalf("OpenAPI contains forbidden route %q", path)
		}
	}
}

func TestExportSecurityAndResponses(t *testing.T) {
	result, err := buildArtifacts()
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]any
	decodeJSON(t, result.openapi, &document)
	paths := document["paths"].(map[string]any)
	for path, raw := range paths {
		for method, operationRaw := range raw.(map[string]any) {
			operation, ok := operationRaw.(map[string]any)
			if !ok || !map[string]bool{"get": true, "post": true, "put": true, "delete": true}[method] {
				continue
			}
			security, hasSecurity := operation["security"]
			if path == "/lobby/v1/sessions" {
				if hasSecurity {
					t.Fatalf("session unexpectedly has security metadata")
				}
			} else if !hasSecurity || len(security.([]any)) != 1 {
				t.Fatalf("%s %s missing bearer security", method, path)
			}
		}
	}
	for path, raw := range paths {
		for method := range raw.(map[string]any) {
			if method != "get" && method != "post" {
				continue
			}
			for _, status := range []string{"200", "401", "403", "404", "409", "422", "500", "503"} {
				assertResponse(t, paths, path, method, status)
			}
		}
	}

	problem := document["components"].(map[string]any)["schemas"].(map[string]any)["Problem"].(map[string]any)
	if problem["properties"].(map[string]any)["code"] == nil {
		t.Fatal("problem schema does not contain code")
	}
}

func TestGenerationEventSchemaSelfContained(t *testing.T) {
	result, err := buildArtifacts()
	if err != nil {
		t.Fatal(err)
	}
	var document, events map[string]any
	decodeJSON(t, result.openapi, &document)
	decodeJSON(t, result.events, &events)
	component := document["components"].(map[string]any)["schemas"].(map[string]any)["GameNextGeneration"].(map[string]any)
	properties := component["properties"].(map[string]any)
	for _, name := range []string{"schema_version", "type", "state"} {
		if properties[name] == nil {
			t.Fatalf("generation event property %q missing", name)
		}
	}
	refs := map[string]bool{}
	collectRefs(events, refs)
	defs, _ := events["$defs"].(map[string]any)
	for ref := range refs {
		if !strings.HasPrefix(ref, "#/$defs/") {
			t.Fatalf("event schema has non-local ref %q", ref)
		}
		if defs[strings.TrimPrefix(ref, "#/$defs/")] == nil {
			t.Fatalf("event schema ref %q is unresolved", ref)
		}
	}
}

func TestCheckModeAndWriteFailure(t *testing.T) {
	result, err := buildArtifacts()
	if err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	openapiPath := filepath.Join(directory, "openapi.json")
	eventsPath := filepath.Join(directory, "events.json")
	if err := os.WriteFile(openapiPath, result.openapi, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(eventsPath, result.events, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"--check", "--output", openapiPath, "--events-output", eventsPath}, &bytes.Buffer{}, &bytes.Buffer{}); err != nil {
		t.Fatalf("matching check failed: %v", err)
	}
	if err := os.WriteFile(eventsPath, []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"--check", "--output", openapiPath, "--events-output", eventsPath}, &bytes.Buffer{}, &bytes.Buffer{}); err == nil {
		t.Fatal("changed check unexpectedly passed")
	}
	before, err := os.ReadFile(openapiPath)
	if err != nil {
		t.Fatal(err)
	}
	badEvents := filepath.Join(directory, "missing", "events.json")
	if err := run([]string{"--output", openapiPath, "--events-output", badEvents}, &bytes.Buffer{}, &bytes.Buffer{}); err == nil {
		t.Fatal("invalid destination unexpectedly succeeded")
	}
	after, err := os.ReadFile(openapiPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("failed write changed the existing OpenAPI artifact")
	}
}

func assertResponse(t *testing.T, paths map[string]any, path, method, status string) {
	t.Helper()
	operation := paths[path].(map[string]any)[method].(map[string]any)
	responses := operation["responses"].(map[string]any)
	if responses[status] == nil {
		t.Fatalf("%s %s missing response %s", method, path, status)
	}
}

func decodeJSON(t *testing.T, data []byte, target any) {
	t.Helper()
	if err := json.Unmarshal(data, target); err != nil {
		t.Fatal(err)
	}
}

func collectRefs(value any, refs map[string]bool) {
	switch typed := value.(type) {
	case map[string]any:
		if ref, ok := typed["$ref"].(string); ok {
			refs[ref] = true
		}
		for _, child := range typed {
			collectRefs(child, refs)
		}
	case []any:
		for _, child := range typed {
			collectRefs(child, refs)
		}
	}
}

func TestExportWireSchemaSemantics(t *testing.T) {
	result, err := buildArtifacts()
	if err != nil {
		t.Fatal(err)
	}
	var document, events map[string]any
	decodeJSON(t, result.openapi, &document)
	decodeJSON(t, result.events, &events)
	schemas := document["components"].(map[string]any)["schemas"].(map[string]any)
	properties := func(name string) map[string]any { return schemas[name].(map[string]any)["properties"].(map[string]any) }
	command := properties("PublicGenerationCommand")
	if command["parameters"].(map[string]any)["$ref"] != "#/components/schemas/SendSoldierGroupParameters" {
		t.Fatal("command parameters are not typed")
	}
	if command["command_type"].(map[string]any)["enum"].([]any)[0] != "send_soldier_group" {
		t.Fatal("command enum missing")
	}
	for _, name := range []string{"origin_persistent_castle_id", "destination_persistent_castle_id", "soldier_amount"} {
		parameter := properties("SendSoldierGroupParameters")[name].(map[string]any)
		minimum := float64(0)
		if name == "soldier_amount" {
			minimum = 1
		}
		if parameter["type"] != "integer" || parameter["minimum"] != minimum {
			t.Fatalf("invalid parameter %s: %+v", name, parameter)
		}
	}
	for _, field := range []string{"game_id", "generation_id", "team_color"} {
		if properties("GenerationState")[field].(map[string]any)["minimum"] != float64(1) {
			t.Fatalf("%s must be positive", field)
		}
	}
	for _, field := range []string{"castles", "soldier_groups", "clashes"} {
		if properties("GenerationView")[field].(map[string]any)["type"] != "array" {
			t.Fatalf("%s must be a nonnullable array", field)
		}
	}
	required := schemas["GenerationView"].(map[string]any)["required"].([]any)
	if len(required) != 2 || required[0] != "castles" || required[1] != "soldier_groups" {
		t.Fatalf("required collections: %+v", required)
	}
	if properties("GameNextGeneration")["state"].(map[string]any)["$ref"] != "#/components/schemas/GenerationState" {
		t.Fatal("event does not reuse HTTP state")
	}
	if properties("GameNextGeneration")["schema_version"].(map[string]any)["enum"].([]any)[0] != float64(1) {
		t.Fatal("event version enum missing")
	}
	paths := document["paths"].(map[string]any)
	for _, test := range []struct{ path, method, model string }{
		{"/generation/commands", "post", "ErrorModel"},
		{"/game/from-lobby/{lobby_id}/generation", "get", "ErrorModel"},
		{"/lobby/v1/connection-token", "post", "Problem"},
	} {
		operation := paths[test.path].(map[string]any)[test.method].(map[string]any)
		for _, status := range []string{"401", "403", "404", "409", "422", "500", "503"} {
			response := operation["responses"].(map[string]any)[status].(map[string]any)
			schema := response["content"].(map[string]any)["application/problem+json"].(map[string]any)["schema"].(map[string]any)
			if schema["$ref"] != "#/components/schemas/"+test.model {
				t.Fatalf("%s %s has incorrect problem model", test.path, status)
			}
		}
	}
	// Event definitions must be the same models, with only reference roots changed.
	for name, definition := range events["$defs"].(map[string]any) {
		expected, err := prettyJSONFrom(schemas[name])
		if err != nil {
			t.Fatal(err)
		}
		actual, err := prettyJSONFrom(definition)
		if err != nil {
			t.Fatal(err)
		}
		expected = bytes.ReplaceAll(expected, []byte(eventSchemaRefPrefix), []byte(eventDefsRefPrefix))
		if !bytes.Equal(actual, expected) {
			t.Fatalf("event definition %s differs from HTTP model", name)
		}
	}
}

func TestCheckedInArtifactsMatch(t *testing.T) {
	if err := run([]string{"--check", "--output", "../../api/game/openapi.json", "--events-output", "../../api/game/events.json"}, &bytes.Buffer{}, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
}
