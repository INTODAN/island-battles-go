package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	islandbattles "github.com/INTODAN/island-battles-go/clients/island-battles-go"
)

func TestOrdersWithValuesAHelperCantTakeAreInvalidArguments(t *testing.T) {
	for _, raw := range []string{
		`{"type": "send_soldier_group", "origin_castle_id": 1, "destination_castle_id": 2, "soldier_amount": 1.5}`,
		`{"type": "send_soldier_group", "origin_castle_id": 1, "destination_castle_id": 2, "soldier_amount": "1"}`,
		`{"type": "send_soldier_group", "origin_castle_id": 1, "destination_castle_id": 2, "soldier_amount": true}`,
		`{"type": "send_soldier_group", "origin_castle_id": -1, "destination_castle_id": 2, "soldier_amount": 1}`,
		`{"type": "send_soldier_group", "origin_castle_id": 1, "soldier_amount": 1}`,
		`{"type": "upgrade_castle", "castle_id": 1, "upgrade_type": 1}`,
		`{"type": "upgrade_castle", "castle_id": true, "upgrade_type": "speed"}`,
		`{"type": "upgrade_castle", "upgrade_type": "speed"}`,
	} {
		if _, err := order(json.RawMessage(raw)); describe(err)["kind"] != "invalid_argument" {
			t.Errorf("%s: got %v", raw, err)
		}
	}
	// The SDK's own check rejects what the helpers build but the contract
	// doesn't allow.
	for _, raw := range []string{
		`{"type": "send_soldier_group", "origin_castle_id": 1, "destination_castle_id": 2, "soldier_amount": 0}`,
		`{"type": "upgrade_castle", "castle_id": 1, "upgrade_type": "armor"}`,
	} {
		command, err := order(json.RawMessage(raw))
		if err != nil {
			t.Fatal(err)
		}
		err = islandbattles.ValidateResult(islandbattles.Result{Commands: []islandbattles.Command{command}})
		if describe(err)["kind"] != "invalid_argument" {
			t.Errorf("%s: got %v", raw, err)
		}
	}
}

func TestErrorsMapOntoKinds(t *testing.T) {
	for _, test := range []struct {
		err    error
		kind   string
		status any
	}{
		{&islandbattles.GameError{Kind: islandbattles.ErrInsufficientSoldiers, Status: 409, Detail: "x"}, "insufficient_soldiers", 409},
		{&islandbattles.GameError{Kind: islandbattles.ErrMemoryTooLarge, Status: 422, Detail: "x"}, "memory_too_large", 422},
		{&islandbattles.GameError{Kind: islandbattles.ErrRejected, Status: 404, Detail: "x"}, "rejected", 404},
		{&islandbattles.GameError{Kind: islandbattles.ErrTransport, Detail: "x"}, "transport", nil},
		{errors.New("the bot's own"), "player", nil},
	} {
		if described := describe(test.err); described["kind"] != test.kind || described["status"] != test.status {
			t.Errorf("%v: got %v", test.err, described)
		}
	}
}

func TestEvaluateAndClose(t *testing.T) {
	var output bytes.Buffer
	adapter := &adapter{output: &output, decisions: make(chan message, 1)}
	adapter.serve(strings.NewReader(`{"type": "evaluate", "id": 3, "generation": {"id": 1, "castles": [], "soldier_groups": [], "clashes": []}}
{"type": "evaluate", "id": 4, "generation": {"id": 0, "castles": [], "soldier_groups": [], "clashes": []}}
{"type": "close"}
`))
	want := `{"distance_left":[],"distances":[],"generation":{"id":1,"castles":[],"soldier_groups":[],"clashes":[]},"id":3,"ok":true,"relations":[],"type":"evaluation"}
{"id":4,"ok":false,"type":"evaluation"}
{"type":"closed"}
`
	if output.String() != want {
		t.Fatalf("got %s", output.String())
	}
}
