package services

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"

	"islandBattles/backend/dal"
	"islandBattles/backend/models"
	"islandBattles/backend/observability"
	"islandBattles/lobby/domain"
	"islandBattles/lobby/realtime"
)

// Watchers are people outside the roster who watch a lobby's game with its
// join code. The generic lobby keeps their rows and authorizes the watch
// channel; Island Battles decides what they read: the board, every turn, the
// teams and the outcome, and the game log only once the game has ended, as
// other teams' lines are for players. A watcher never plays, so they never
// count toward the turn barrier or a deadline.

// watchState builds what watchers see of a generation.
func watchState(game *models.Game, generation *models.Generation) (models.WatchState, error) {
	channel, err := domain.WatchChannel(domain.Scope{GameKey: models.LobbyGameKey, LobbyID: lobbyID(game)})
	if err != nil {
		return models.WatchState{}, err
	}
	teams := game.Teams
	if teams == nil {
		teams = []models.GameTeam{}
	}
	return models.WatchState{GameID: game.ID, LobbyID: lobbyID(game), GenerationID: generation.ID, Channel: channel, Teams: teams,
		Generation: GenerationView(generation), Outcome: outcomeView(game), TurnDeadlineAt: turnDeadline(game, generation)}, nil
}

// watchEvent is the watch channel's event for a generation, which the turn
// that created it pushes with the players' events.
func watchEvent(game *models.Game, generation *models.Generation) (realtime.Publication, error) {
	state, err := watchState(game, generation)
	if err != nil {
		return realtime.Publication{}, err
	}
	var event any = models.WatchGenerationEvent{SchemaVersion: models.GenerationEventSchemaVersion, Type: models.GenerationEventType, State: state}
	if state.Outcome != nil {
		event = models.WatchGameEndedEvent{SchemaVersion: models.GenerationEventSchemaVersion, Type: models.GameEndedEventType, State: state}
	}
	payload, err := json.Marshal(event)
	if err != nil {
		return realtime.Publication{}, err
	}
	return realtime.Publication{Channel: state.Channel, Data: payload}, nil
}

// authorizeWatcher admits the lobby's watchers that the host hasn't kicked
// and its active members, which the lobby checks, and the game's roster
// players, whose lobby may be gone. A kicked watcher gets 403, and anyone
// else 404, as for a game that doesn't exist.
func (service *GenerationService) authorizeWatcher(ctx context.Context, principal domain.Principal, lobbyID string) (*models.Game, error) {
	var err error = domain.ErrNotFound
	if service.Watchers != nil {
		err = service.Watchers.AuthorizeWatch(ctx, principal, domain.Scope{GameKey: models.LobbyGameKey, LobbyID: lobbyID})
	}
	switch {
	case errors.Is(err, domain.ErrWatcherKicked):
		return nil, reject(403, domain.ErrWatcherKicked.Message)
	case err != nil && !errors.Is(err, domain.ErrNotFound):
		return nil, err
	}
	game, findError := dal.FindLobbyGame(service.Database.WithContext(ctx), lobbyID)
	if errors.Is(findError, domain.ErrNotFound) {
		return nil, reject(404, "game not found")
	}
	if findError != nil {
		return nil, findError
	}
	observability.Annotate(ctx, slog.Uint64("game_id", uint64(game.ID)))
	if err != nil {
		// Not a watcher or a member: a roster player still reads it.
		if rosterError := service.authorize(ctx, principal, game); rosterError != nil {
			return nil, rosterError
		}
	}
	return game, nil
}

// Watch returns a lobby game's current generation as watchers see it. Like
// the other game reads, it first applies an overdue turn deadline and lets
// Go bots catch up for at most goBotReadBudget.
func (service *GenerationService) Watch(ctx context.Context, principal domain.Principal, lobbyID string) (models.WatchState, error) {
	game, err := service.authorizeWatcher(ctx, principal, lobbyID)
	if err != nil {
		return models.WatchState{}, err
	}
	if game, err = service.applyOverdueDeadline(ctx, game); err != nil {
		return models.WatchState{}, err
	}
	generation, err := dal.GetLatestGenerationForGame(service.Database.WithContext(ctx), game.ID)
	if err != nil {
		return models.WatchState{}, err
	}
	return watchState(game, generation)
}

// WatchDemo returns a lobby game's history for watchers, in the demo format:
// every generation so far, and the game log lines only once the game has
// ended. Until then each frame's hidden_team_colors names every team.
func (service *GenerationService) WatchDemo(ctx context.Context, principal domain.Principal, lobbyID string) (models.Demo, error) {
	game, err := service.authorizeWatcher(ctx, principal, lobbyID)
	if err != nil {
		return models.Demo{}, err
	}
	// A watcher belongs to no team: the zero player's team colour is no
	// team's.
	return service.demo(ctx, game, models.FrozenGamePlayer{})
}
