package islandbattles

import (
	_ "embed"
	"fmt"
)

// fixtureTurnJSON is the fixture turn of the Python SDK
// (island_battles_client/fixture.py); a Python test keeps the two equal.
//
//go:embed fixture_turn.json
var fixtureTurnJSON []byte

// FixtureTurn is a small turn for trying a bot: team 1 against team 2, a
// neutral castle and one enemy soldier group. It is the game's first turn,
// turn 1, without memory, on the generation with ID 12, since the server's
// generation IDs don't start at 1. The check subcommand plays it, and the
// fixture subcommand prints it.
func FixtureTurn() Turn {
	turn, err := DecodeTurn(fixtureTurnJSON)
	if err != nil {
		panic(fmt.Sprintf("the embedded fixture turn is invalid: %v", err))
	}
	return turn
}
