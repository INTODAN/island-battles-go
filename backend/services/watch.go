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
	return models.WatchState{GameID: game.ID, LobbyID: lobbyID(game), LobbyRound: game.LobbyRound, GenerationID: generation.ID, Channel: channel, Teams: teams,
		Generation: GenerationView(generation), Outcome: outcomeView(game), TurnDeadlineAt: turnDeadline(game, generation)}, nil
}

// watchEvent is the watch channel's event for a generation, which the turn
// that created it pushes with the players' events.
func watchEvent(game *models.Game, generation *models.Generation) (Publication, error) {
	state, err := watchState(game, generation)
	if err != nil {
		return Publication{}, err
	}
	var event any = models.WatchGenerationEvent{SchemaVersion: models.GenerationEventSchemaVersion, Type: models.GenerationEventType, State: state}
	if state.Outcome != nil {
		event = models.WatchGameEndedEvent{SchemaVersion: models.GenerationEventSchemaVersion, Type: models.GameEndedEventType, State: state}
	}
	payload, err := json.Marshal(event)
	if err != nil {
		return Publication{}, err
	}
	return Publication{Channel: state.Channel, Data: payload}, nil
}

// authorizeWatcher admits the game's roster players, whose lobby may be
// gone, and then the lobby's watchers that the host hasn't kicked and its
// active members, which the lobby checks. The roster comes first: a roster
// player always reads the watch views, whatever their watcher row says and
// whether or not the lobby store answers. A kicked watcher who isn't on the
// roster gets 403, and anyone else 404, as for a game that doesn't exist.
func (service *GenerationService) authorizeWatcher(ctx context.Context, principal domain.Principal, lobbyID string) (*models.Game, error) {
	game, findError := dal.FindLobbyGame(service.Database.WithContext(ctx), lobbyID)
	if errors.Is(findError, domain.ErrNotFound) {
		return nil, reject(404, "game not found")
	}
	if findError != nil {
		return nil, findError
	}
	observability.Annotate(ctx, slog.Uint64("game_id", uint64(game.ID)))
	rosterError := service.authorize(ctx, principal, game)
	if rosterError == nil {
		return game, nil
	}
	var rejected *GenerationError
	if !errors.As(rosterError, &rejected) || rejected.Status != 404 {
		return nil, rosterError
	}
	var err error = domain.ErrNotFound
	if service.Watchers != nil {
		err = service.Watchers.AuthorizeWatch(ctx, principal, domain.Scope{GameKey: models.LobbyGameKey, LobbyID: lobbyID})
	}
	switch {
	case err == nil:
		return game, nil
	case errors.Is(err, domain.ErrWatcherKicked):
		return nil, reject(403, domain.ErrWatcherKicked.Message)
	case errors.Is(err, domain.ErrNotFound):
		return nil, rosterError
	}
	return nil, err
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

// WatchReplay derives the replay of a lobby's game for watchers, as Replay
// does for roster players. It is the page a watcher sees in the lobby page's
// frame; the watcher's frame reads the history through WatchDemo.
func (service *GenerationService) WatchReplay(ctx context.Context, principal domain.Principal, lobbyID string) (GameReplay, error) {
	game, err := service.authorizeWatcher(ctx, principal, lobbyID)
	if err != nil {
		return GameReplay{}, err
	}
	if game, err = service.applyOverdueDeadline(ctx, game); err != nil {
		return GameReplay{}, err
	}
	generations, err := dal.LoadGameGenerations(service.Database.WithContext(ctx), game.ID)
	if err != nil {
		return GameReplay{}, err
	}
	return gameReplay(game, generations), nil
}
