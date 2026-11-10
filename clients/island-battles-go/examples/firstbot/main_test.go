package main

import (
	"context"
	"testing"

	islandbattles "github.com/INTODAN/island-battles-go/clients/island-battles-go"
)

// The README's first bot plays the fixture turn and counts its turns.
func TestFirstBotPlaysTheFixtureTurn(t *testing.T) {
	turn := islandbattles.FixtureTurn()
	result, err := play(context.Background(), turn)
	if err != nil {
		t.Fatal(err)
	}
	if err := islandbattles.ValidateResult(result); err != nil {
		t.Fatal(err)
	}
	if len(result.Commands) != 1 || string(result.Commands[0].Parameters) != `{"origin_castle_id":0,"destination_castle_id":1,"soldier_amount":1}` {
		t.Fatalf("got commands %+v", result.Commands)
	}
	turn.Memory = result.Memory
	if result, err = play(context.Background(), turn); err != nil || string(result.Memory) != `{"turns":2}` {
		t.Fatalf("got memory %s and error %v", result.Memory, err)
	}
}
