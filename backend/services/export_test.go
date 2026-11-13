package services

import (
	"context"
	"testing"
	"time"

	"islandBattles/backend/models"
	"islandBattles/lobby/domain"

	"gorm.io/gorm"
)

// RunLobbyGameWithGoBots starts a lobby's game with the default bot levels
// that the backend plays for some roster players, by player ID, as the
// players' bot choices and the lobby's bot seats do.
func RunLobbyGameWithGoBots(ctx context.Context, database *gorm.DB, publisher Publisher, snapshot domain.Snapshot, levels map[string]uint) (models.BindingResult, error) {
	chosen := make(map[string]frozenBot, len(levels))
	for playerID, level := range levels {
		chosen[playerID] = defaultBot(level)
	}
	return runLobbyGame(ctx, database, publisher, snapshot, chosen)
}

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
