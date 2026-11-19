package services

import (
	"context"
	"log/slog"
	"slices"
	"time"

	"github.com/INTODAN/mpl/domain"
	"islandBattles/backend/dal"
	"islandBattles/backend/models"
	"islandBattles/backend/observability"
	"islandBattles/backend/submittoken"

	"gorm.io/gorm"
)

// submitTokenGrace is how long after the turn's deadline a submit token still
// verifies, so a batch sent just before the deadline isn't refused for its
// token on the way. The deadline itself still applies to the batch.
const submitTokenGrace = 30 * time.Second

// submitTokenNoLimit is how long after its generation a submit token
// verifies when turns have no time limit.
const submitTokenNoLimit = 24 * time.Hour

// TurnDelivery builds the caller's turn of the lobby game's open generation,
// as a bot that runs outside the backend is given it: the BotTurn, and a
// submit token for submitURL that submits exactly this turn. With seatID, a
// roster player other than the caller, it builds that player's turn instead:
// a localhost bot seat that the caller relays (models.RelayedBots), or a 403
// for any other player. Like the state read, it lets Go bots catch up and
// then ends an overdue turn, so in a real-time game it delivers the turn that
// follows. A player with nothing to play
// gets a 409: the game has ended, with GameEndedProblemType, their team
// resigned or left a real-time game, with TeamOutProblemType, their team is
// eliminated, or they have completed the turn.
func (service *GenerationService) TurnDelivery(ctx context.Context, principal domain.Principal, lobbyID, seatID string, tokens *submittoken.Signer, submitURL string) (models.TurnDelivery, error) {
	database := service.Database.WithContext(ctx)
	game, err := dal.FindLobbyGame(database, lobbyID)
	if err != nil {
		return models.TurnDelivery{}, err
	}
	observability.Annotate(ctx, slog.Uint64("game_id", uint64(game.ID)))
	if err = service.authorize(ctx, principal, game); err != nil {
		return models.TurnDelivery{}, err
	}
	if game.Outcome == nil {
		if _, err = service.playGoBotTurns(ctx, game.ID, goBotReadBudget); err != nil {
			return models.TurnDelivery{}, err
		}
	}
	var delivery models.TurnDelivery
	var end turnEnd
	// A turn that ends the game here still commits the ending.
	var nothingToPlay error
	err = dal.WithLockedGame(database, game.ID, func(transaction *gorm.DB, locked *models.Game) error {
		players, err := dal.LoadFrozenPlayers(transaction, locked.ID)
		if err != nil {
			return err
		}
		player, err := frozenPlayer(players, principal.PlayerID)
		if err != nil {
			return err
		}
		if seatID != "" && seatID != principal.PlayerID {
			if player, err = relayedSeat(players, principal.PlayerID, seatID); err != nil {
				return err
			}
		}
		generation, err := dal.GetLatestGenerationForGame(transaction, locked.ID)
		if err != nil {
			return err
		}
		observability.Annotate(ctx, slog.Uint64("generation_id", uint64(generation.ID)))
		if end, err = endTurn(ctx, transaction, locked, generation, players, principal.PlayerID, service.now(), service.Bots); err != nil {
			return err
		}
		generation = end.generation
		if locked.Outcome != nil {
			nothingToPlay = &GenerationError{Status: 409, Detail: "the game has ended: " + describeOutcome(locked), Type: GameEndedProblemType}
			return nil
		}
		if departure := teamDeparture(locked, player.TeamColor); departure != nil {
			nothingToPlay = teamOut(locked, *departure)
			return nil
		}
		if !slices.Contains(standingTeams(generation, locked.Teams), player.TeamColor) {
			nothingToPlay = reject(409, "your team has been eliminated, so it has no turn to play")
			return nil
		}
		inputs, err := dal.LoadGenerationInputs(transaction, generation.ID)
		if err != nil {
			return err
		}
		if slices.ContainsFunc(inputs, func(input models.GenerationInput) bool { return input.PlayerID == player.PlayerID && input.Completed }) {
			nothingToPlay = reject(409, "player turn is already complete")
			return nil
		}
		memory, err := dal.FindBotMemory(transaction, locked.ID, player.PlayerID)
		if err != nil {
			return err
		}
		delivery, err = newTurnDelivery(locked, generation, player, inputs, memory, tokens, submitURL)
		return err
	})
	if err != nil {
		return models.TurnDelivery{}, err
	}
	service.afterTurnEnd(ctx, game.ID, end, goBotReadBudget)
	if nothingToPlay != nil {
		return models.TurnDelivery{}, nothingToPlay
	}
	return delivery, nil
}

// relayedSeat is the roster player seatID, a localhost bot seat that relayer
// relays, or a 403.
func relayedSeat(players []models.FrozenGamePlayer, relayer, seatID string) (models.FrozenGamePlayer, error) {
	for _, relayed := range models.RelayedBots(players, relayer) {
		if relayed.PlayerID == seatID {
			return frozenPlayer(players, seatID)
		}
	}
	return models.FrozenGamePlayer{}, reject(403, "player_id is not a localhost bot seat that you relay: only the player who added a localhost bot seat relays it, and GenerationState.relayed_bots lists yours")
}

// AuthorizeSubmitToken checks that a verified submit token's player is on the
// roster of the token's game. A token of a player who isn't, or of a game
// that is gone, is refused as a whole, with ErrInvalid.
func (service *GenerationService) AuthorizeSubmitToken(ctx context.Context, claims submittoken.Claims) (domain.Principal, error) {
	player, err := dal.FindFrozenPlayer(service.Database.WithContext(ctx), claims.GameID, claims.PlayerID)
	if err != nil {
		return domain.Principal{}, err
	}
	if player == nil {
		return domain.Principal{}, submittoken.ErrInvalid
	}
	return domain.Principal{PlayerID: claims.PlayerID}, nil
}
