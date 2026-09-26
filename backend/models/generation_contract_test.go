package models_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/humatest"
	"islandBattles/backend/models"
)

// Exercise the actual Huma input boundary without requiring game persistence.
// The raw internal command representation must still enforce its typed schema.
func TestPublicCommandSchemaValidation(t *testing.T) {
	_, api := humatest.New(t)
	calls := 0
	huma.Register(api, huma.Operation{OperationID: "testCommandContract", Method: "POST", Path: "/commands"}, func(_ context.Context, input *struct {
		Body models.RegisterGenerationCommands
	}) (*struct {
		Body models.RegisterGenerationCommands
	}, error) { calls++; return &struct {
		Body models.RegisterGenerationCommands
	}{Body: input.Body}, nil })
	tests := []struct {
		name, command string
		status        int
	}{
		{"persistent zero ID", `{"command_type":"send_soldier_group","parameters":{"origin_persistent_castle_id":0,"destination_persistent_castle_id":1,"soldier_amount":1}}`, 200},
		{"missing origin", `{"command_type":"send_soldier_group","parameters":{"destination_persistent_castle_id":1,"soldier_amount":1}}`, 422},
		{"null origin", `{"command_type":"send_soldier_group","parameters":{"origin_persistent_castle_id":null,"destination_persistent_castle_id":1,"soldier_amount":1}}`, 422},
		{"boolean origin", `{"command_type":"send_soldier_group","parameters":{"origin_persistent_castle_id":false,"destination_persistent_castle_id":1,"soldier_amount":1}}`, 422},
		{"string origin", `{"command_type":"send_soldier_group","parameters":{"origin_persistent_castle_id":"0","destination_persistent_castle_id":1,"soldier_amount":1}}`, 422},
		{"negative origin", `{"command_type":"send_soldier_group","parameters":{"origin_persistent_castle_id":-1,"destination_persistent_castle_id":1,"soldier_amount":1}}`, 422},
		{"zero amount", `{"command_type":"send_soldier_group","parameters":{"origin_persistent_castle_id":0,"destination_persistent_castle_id":1,"soldier_amount":0}}`, 422},
		{"fractional amount", `{"command_type":"send_soldier_group","parameters":{"origin_persistent_castle_id":0,"destination_persistent_castle_id":1,"soldier_amount":1.5}}`, 422},
		{"unknown field", `{"command_type":"send_soldier_group","parameters":{"origin_persistent_castle_id":0,"destination_persistent_castle_id":1,"soldier_amount":1,"admin":true}}`, 422},
		{"server-only command", `{"command_type":"move_soldier_groups","parameters":{}}`, 422},
		{"null parameters", `{"command_type":"send_soldier_group","parameters":null}`, 422},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			body := fmt.Sprintf(`{"game_id":1,"generation_id":1,"request_id":"10000000-0000-4000-8000-000000000000","commands":[%s]}`, test.command)
			response := api.Post("/commands", strings.NewReader(body))
			if response.Code != test.status {
				t.Fatalf("status %d, want %d: %s", response.Code, test.status, response.Body)
			}
		})
	}
	if calls != 1 {
		t.Fatalf("handler called %d times; invalid payloads reached handler", calls)
	}
}
