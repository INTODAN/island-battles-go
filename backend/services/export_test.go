package services

import (
	"context"
	"testing"
	"time"

	"github.com/INTODAN/mpl/domain"
	"islandBattles/backend/models"

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
	return runLobbyGame(ctx, database, publisher, nil, snapshot, chosen)
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

// ServerBot is a bot that the server plays for a roster player in
// RunLobbyGameWithServerBots: a default bot of Level, a hosted bot at URL, or
// the lobby's uploaded package PackageID.
type ServerBot struct {
	Level     uint
	URL       string
	PackageID string
}

// RunLobbyGameWithServerBots starts a lobby's game with the bots that the
// server plays for some roster players, by player ID, and sends the first
// turns of the hosted and uploaded ones with sender.
func RunLobbyGameWithServerBots(ctx context.Context, database *gorm.DB, publisher Publisher, sender *TurnSender, snapshot domain.Snapshot, chosenBots map[string]ServerBot) (models.BindingResult, error) {
	chosen := make(map[string]frozenBot, len(chosenBots))
	for playerID, bot := range chosenBots {
		switch {
		case bot.Level != 0:
			chosen[playerID] = defaultBot(bot.Level)
		case bot.URL != "":
			chosen[playerID] = frozenBot{Kind: models.HostedBot, URL: bot.URL}
		default:
			chosen[playerID] = frozenBot{Kind: models.UploadedBot, PackageID: bot.PackageID}
		}
	}
	return runLobbyGame(ctx, database, publisher, sender, snapshot, chosen)
}

// SentTurnPlayers lists the players whose turns sender would send for the
// open turn, generation, in roster order.
func SentTurnPlayers(sender *TurnSender, game *models.Game, generation *models.Generation, players []models.FrozenGamePlayer, inputs []models.GenerationInput) ([]string, error) {
	turns, err := sender.sentTurns(game, generation, players, inputs, nil)
	playerIDs := make([]string, 0, len(turns))
	for _, turn := range turns {
		playerIDs = append(playerIDs, turn.player.PlayerID)
	}
	return playerIDs, err
}
