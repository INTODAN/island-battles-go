package services

import (
	"testing"
	"time"
)

// RunLobbyGameWithGoBots starts a lobby's game with the default bot levels
// that the backend plays for some roster players, by player ID, as the
// players' bot choice (#111) and bot seats (#115) will.
var RunLobbyGameWithGoBots = runLobbyGame

// SetGoBotTimeBudget changes how long one request plays the turns of a game
// that only Go bots play, until the test ends. Zero plays one turn per
// request.
func SetGoBotTimeBudget(t *testing.T, budget time.Duration) {
	previous := goBotTimeBudget
	goBotTimeBudget = budget
	t.Cleanup(func() { goBotTimeBudget = previous })
}
