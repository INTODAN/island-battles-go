package services

import (
	"context"

	"github.com/INTODAN/mpl/domain"
	"islandBattles/backend/dal"
	"islandBattles/backend/models"
)

// authorize admits only the players frozen into the game's roster at start.
// A started game never reads its lobby, so it stays playable after the lobby
// is deleted. Checking before WithLockedGame keeps outsiders off the game lock.
func (service *GenerationService) authorize(ctx context.Context, principal domain.Principal, game *models.Game) error {
	if game == nil || game.LobbyID == nil {
		return reject(404, "game not found")
	}
	player, err := dal.FindFrozenPlayer(service.Database.WithContext(ctx), game.ID, principal.PlayerID)
	if err != nil {
		return err
	}
	if player == nil {
		return reject(404, "game not found")
	}
	return nil
}

// Replay derives the replay of a lobby's game from its stored generations,
// for its roster players. It takes no game lock: stored generations never
// change, and the game is read first, so a concurrent turn can at most add a
// frame to an older outcome.
func (service *GenerationService) Replay(ctx context.Context, principal domain.Principal, lobbyID string) (GameReplay, error) {
	database := service.Database.WithContext(ctx)
	game, err := dal.FindLobbyGame(database, lobbyID)
	if err != nil {
		return GameReplay{}, err
	}
	if err := service.authorize(ctx, principal, game); err != nil {
		return GameReplay{}, err
	}
	if game, err = service.applyOverdueDeadline(ctx, game); err != nil {
		return GameReplay{}, err
	}
	generations, err := dal.LoadGameGenerations(database, game.ID)
	if err != nil {
		return GameReplay{}, err
	}
	return gameReplay(game, generations), nil
}
