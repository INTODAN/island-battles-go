package services

import (
	"context"
	"log/slog"

	"github.com/INTODAN/mpl/domain"
	"islandBattles/backend/dal"
	"islandBattles/backend/demotoken"
	"islandBattles/backend/models"
	"islandBattles/backend/observability"
)

// GameNotEndedProblemType is the problem type of the 409 that answers a
// game-ended delivery read while the game still runs.
const GameNotEndedProblemType = "urn:island-battles:problem:game-not-ended"

// GameEndedDelivery builds the end of the lobby game as the caller's bot is
// told it (#413), with a demo link under origin: the delivery the server
// sends a hosted or uploaded bot when the game ends, for a client to relay to
// a localhost bot. With seatID it builds that of a localhost bot seat that
// the caller relays (models.RelayedBots), or a 403 for any other player.
// Like every read, it first ends an overdue turn, which can end the game. A
// game that still runs is a 409 with GameNotEndedProblemType.
func (service *GenerationService) GameEndedDelivery(ctx context.Context, principal domain.Principal, lobbyID, seatID string, links DemoLinks, origin string) (models.GameEndedDelivery, error) {
	database := service.Database.WithContext(ctx)
	game, err := dal.FindLobbyGame(database, lobbyID)
	if err != nil {
		return models.GameEndedDelivery{}, err
	}
	observability.Annotate(ctx, slog.Uint64("game_id", uint64(game.ID)))
	if err = service.authorize(ctx, principal, game); err != nil {
		return models.GameEndedDelivery{}, err
	}
	if game, err = service.applyOverdueDeadline(ctx, game, principal.PlayerID); err != nil {
		return models.GameEndedDelivery{}, err
	}
	players, err := dal.LoadFrozenPlayers(database, game.ID)
	if err != nil {
		return models.GameEndedDelivery{}, err
	}
	player, err := frozenPlayer(players, principal.PlayerID)
	if err != nil {
		return models.GameEndedDelivery{}, err
	}
	if seatID != "" && seatID != principal.PlayerID {
		if player, err = relayedSeat(players, principal.PlayerID, seatID); err != nil {
			return models.GameEndedDelivery{}, err
		}
	}
	if game.Outcome == nil {
		return models.GameEndedDelivery{}, &GenerationError{Status: 409, Detail: "the game hasn't ended yet", Type: GameNotEndedProblemType}
	}
	generation, err := dal.GetLatestGenerationForGame(database, game.ID)
	if err != nil {
		return models.GameEndedDelivery{}, err
	}
	memory, err := dal.FindBotMemory(database, game.ID, player.PlayerID)
	if err != nil {
		return models.GameEndedDelivery{}, err
	}
	ended, err := newGameEndedDelivery(game, generation, player, memory)
	if err != nil {
		return models.GameEndedDelivery{}, err
	}
	if ended.Demo, err = links.link(origin, game, generation, player.PlayerID); err != nil {
		return models.GameEndedDelivery{}, err
	}
	return ended, nil
}

// GameDemo saves a game, found by its ID, as a demo for a roster player, as
// Demo does by its lobby. Its caller has a player token, or a demo token
// that AuthorizeDemoToken turned into its player.
func (service *GenerationService) GameDemo(ctx context.Context, principal domain.Principal, gameID uint) (models.Demo, error) {
	database := service.Database.WithContext(ctx)
	game, err := dal.FindGame(database, gameID)
	if err != nil {
		return models.Demo{}, err
	}
	observability.Annotate(ctx, slog.Uint64("game_id", uint64(game.ID)))
	if err := service.authorize(ctx, principal, game); err != nil {
		return models.Demo{}, err
	}
	caller, err := dal.FindFrozenPlayer(database, game.ID, principal.PlayerID)
	if err != nil {
		return models.Demo{}, err
	}
	if caller == nil {
		return models.Demo{}, reject(404, "game not found")
	}
	return service.demo(ctx, game, *caller)
}

// AuthorizeDemoToken checks that a verified demo token's player is on the
// roster of the token's game. A token of a player who isn't, or of a game
// that is gone, is refused as a whole, with demotoken.ErrInvalid.
func (service *GenerationService) AuthorizeDemoToken(ctx context.Context, claims demotoken.Claims) (domain.Principal, error) {
	player, err := dal.FindFrozenPlayer(service.Database.WithContext(ctx), claims.GameID, claims.PlayerID)
	if err != nil {
		return domain.Principal{}, err
	}
	if player == nil {
		return domain.Principal{}, demotoken.ErrInvalid
	}
	return domain.Principal{PlayerID: claims.PlayerID}, nil
}
