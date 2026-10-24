package services

import (
	"context"

	"islandBattles/backend/dal"
	"islandBattles/backend/models"
	"islandBattles/lobby/domain"
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

// Replay returns the saved replay of a lobby's game to its roster players.
func (service *GenerationService) Replay(ctx context.Context, principal domain.Principal, lobbyID string) ([]byte, error) {
	game, err := dal.FindLobbyGame(service.Database.WithContext(ctx), lobbyID)
	if err != nil {
		return nil, err
	}
	if err := service.authorize(ctx, principal, game); err != nil {
		return nil, err
	}
	return game.Replay, nil
}
