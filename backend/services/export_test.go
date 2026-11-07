package services

import (
	"testing"
	"time"
)

// RunLobbyGameWithGoBots starts a lobby's game with the default bot levels
// that the backend plays for some roster players, by player ID, as the
// players' bot choice (#111) and bot seats (#115) will.
var RunLobbyGameWithGoBots = runLobbyGame

// SetGoBotTimeBudgets changes how long one request plays the turns of a game
// that only Go bots play, for writes and for reads, until the test ends. Zero
// plays one turn per request.
func SetGoBotTimeBudgets(t *testing.T, write, read time.Duration) {
	previousWrite, previousRead := goBotWriteBudget, goBotReadBudget
	goBotWriteBudget, goBotReadBudget = write, read
	t.Cleanup(func() { goBotWriteBudget, goBotReadBudget = previousWrite, previousRead })
}

// NewGoBotTurn exposes newGoBotTurn to the external tests.
var NewGoBotTurn = newGoBotTurn
