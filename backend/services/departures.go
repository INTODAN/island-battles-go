package services

import (
	"context"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"time"

	"github.com/INTODAN/mpl/domain"
	"islandBattles/backend/dal"
	"islandBattles/backend/models"
	"islandBattles/backend/observability"
	"islandBattles/backend/simulation"

	"gorm.io/gorm"
)

// A real-time game can end without a conquest, by three rules that apply only
// to real-time games (#347):
//
//   - A player can resign (Resign). The team leaves when the open turn plays.
//   - A player the server hasn't heard from for the game's silence limit by the
//     time a turn ends has left (leaveSilentTeams), and their team leaves in
//     that turn, exactly as a resigning team does.
//   - A game that nobody played for its turn limit is abandoned (abandonGame):
//     it ends on its open generation, and the most castles win.
//
// A team that leaves is recorded in Game.Departures. The turn that takes it
// out (playTurn) starts with simulation.RemoveTeamParams for it, which makes
// its castles neutral and removes its soldier groups, and drops its own
// commands. It is then out wherever the code asks which teams are still
// standing, and the end rule runs as after any turn.

// SeenWriteInterval is how often, at most, the server records that it heard
// from a player (FrozenGamePlayer.LastSeenAt).
const SeenWriteInterval = 2 * time.Second

// silenceMargin is how much longer than one turn a player may go unheard
// before a short turn limit takes them out (silenceLimit). A client that calls
// once per turn is unheard at a turn's end for less than one pace plus
// SeenWriteInterval, whatever its latency, because a late call also delays the
// next turn; the rest is room for the other players' latency.
const silenceMargin = 5 * time.Second

// silenceLimit is how long the server may go without hearing from a player of
// a real-time game, measured at the end of a turn, before the player has left:
// the game's turn limit, or one turn plus silenceMargin when that is longer,
// so that a client that calls once per turn is never taken out.
func silenceLimit(game *models.Game) time.Duration {
	limit := time.Duration(*game.TurnTimeoutSeconds) * time.Second
	if game.TurnPaceMilliseconds != nil {
		limit = max(limit, time.Duration(*game.TurnPaceMilliseconds)*time.Millisecond+silenceMargin)
	}
	return limit
}

// markSeen records that the server heard from player, a roster player who made
// a game request, in a real-time game with a turn limit that is still running.
// It writes at most once every SeenWriteInterval per player, outside the game
// lock, so that a batch refused afterwards still counts.
func (service *GenerationService) markSeen(ctx context.Context, game *models.Game, player models.FrozenGamePlayer) error {
	if game.TurnPaceMilliseconds == nil || game.TurnTimeoutSeconds == nil || game.Outcome != nil {
		return nil
	}
	now := service.now()
	staleSince := now.Add(-SeenWriteInterval)
	if player.LastSeenAt.After(staleSince) {
		return nil
	}
	_, err := dal.MarkPlayerSeen(service.Database.WithContext(ctx), game.ID, player.PlayerID, now, staleSince)
	return err
}

// abandoned reports whether nobody has played a running real-time game for
// its turn limit: its open turn, generation, ended more than the limit before
// now. A request that finds a turn overdue plays one turn, so only a game that
// no request reached for that long gets here. Without a limit a game is never
// abandoned.
func abandoned(game *models.Game, generation *models.Generation, now time.Time) bool {
	if game.Outcome != nil || game.TurnPaceMilliseconds == nil || game.TurnTimeoutSeconds == nil {
		return false
	}
	deadline := turnDeadline(game, generation)
	return deadline != nil && now.Sub(*deadline) > time.Duration(*game.TurnTimeoutSeconds)*time.Second
}

// abandonGame ends a real-time game that nobody played on its open
// generation, generation, under the game lock, as the generation cap would:
// among the teams still standing, except a team whose resignation was waiting
// for the turn, the team with the most castles wins, and equal counts, or no
// such team, are a draw (simulation.AbandonedOutcome). No turn is played. It
// returns the game.ended events to push once the transaction commits.
func abandonGame(transaction *gorm.DB, game *models.Game, generation *models.Generation, players []models.FrozenGamePlayer) ([]Publication, error) {
	board, err := dal.LoadGameState(transaction, game.ID)
	if err != nil {
		return nil, err
	}
	resigned := pendingDepartures(game)
	outcome := simulation.AbandonedOutcome(board, game.Teams, resigned)
	for _, color := range simulation.StandingTeams(board, game.Teams) {
		if !slices.Contains(resigned, color) {
			outcome.Survivors = append(outcome.Survivors, color)
		}
	}
	outcome.Departures = slices.Clone(game.Departures)
	if err := rateGame(transaction, game, players, outcome, 0); err != nil {
		return nil, err
	}
	if err := dal.SaveGameOutcome(transaction, game, *outcome); err != nil {
		return nil, err
	}
	inputs, err := dal.LoadGenerationInputs(transaction, generation.ID)
	if err != nil {
		return nil, err
	}
	memories, err := dal.LoadBotMemories(transaction, game.ID)
	if err != nil {
		return nil, err
	}
	return generationEvents(game, generation, players, inputs, memories)
}

// leaveSilentTeams takes out the teams of a real-time game whose player the
// server hasn't heard from, as the turn that ends at deadline is played: it
// adds their departures to game.Departures, for playTurn to apply. A standing
// team is heard when its player is a Go bot, which the server plays itself,
// is caller, who made the request, or was last seen no more than
// silenceLimit before the turn's end. Measuring at the turn's end, not at the
// request, charges nobody for the time in which nobody played the game. A
// team that already resigned stays resigned. When no standing team is heard
// at all, nobody played the game, so it reports that, for the caller to end
// the game as abandoned, and takes nobody out. Without a turn limit, nobody
// leaves.
func leaveSilentTeams(game *models.Game, generation *models.Generation, players []models.FrozenGamePlayer, caller string, deadline time.Time) (nobodyHeard bool) {
	if game.TurnTimeoutSeconds == nil {
		return false
	}
	limit := silenceLimit(game)
	standing := standingTeams(generation, game.Teams)
	heard, silent := map[uint]bool{}, map[uint][]string{}
	for _, player := range players {
		if !slices.Contains(standing, player.TeamColor) {
			continue
		}
		if isGoBot(player) || player.PlayerID == caller || deadline.Sub(player.LastSeenAt) <= limit {
			heard[player.TeamColor] = true
		} else {
			silent[player.TeamColor] = append(silent[player.TeamColor], player.DisplayName)
		}
	}
	if len(heard) == 0 && len(silent) > 0 {
		return true
	}
	for _, color := range slices.Sorted(maps.Keys(silent)) {
		if heard[color] || teamDeparture(game, color) != nil {
			continue
		}
		slices.Sort(silent[color])
		game.Departures = append(game.Departures, models.Departure{Color: color, Kind: models.DepartureLeft, Players: silent[color]})
	}
	return false
}

// pendingDepartures lists the colours of the teams whose departure waits for
// the open turn to play.
func pendingDepartures(game *models.Game) []uint {
	var colors []uint
	for _, departure := range game.Departures {
		if departure.Turn == nil {
			colors = append(colors, departure.Color)
		}
	}
	return colors
}

// applyDepartures records that the turn which created generation turn took
// out the teams whose departure waited for it.
func applyDepartures(transaction *gorm.DB, game *models.Game, turn int) error {
	for index := range game.Departures {
		if game.Departures[index].Turn == nil {
			applied := turn
			game.Departures[index].Turn = &applied
		}
	}
	return dal.SaveGameDepartures(transaction, game)
}

// teamDeparture is the departure of the team with color, or nil while the
// team hasn't resigned or left.
func teamDeparture(game *models.Game, color uint) *models.Departure {
	for index := range game.Departures {
		if game.Departures[index].Color == color {
			return &game.Departures[index]
		}
	}
	return nil
}

// teamOut refuses a request of a player whose team resigned or left.
func teamOut(game *models.Game, departure models.Departure) error {
	var detail string
	switch {
	case departure.Kind == models.DepartureLeft:
		detail = fmt.Sprintf("your team left the game: the server didn't hear from %s for %s", joinNames(departure.Players), describeDuration(silenceLimit(game)))
	case departure.Turn == nil:
		detail = "your team resigned: it leaves the game when the open turn plays, and its commands are no longer played"
	default:
		detail = "your team resigned, so it is out of the game"
	}
	return &GenerationError{Status: 409, Detail: detail, Type: TeamOutProblemType}
}

// departureViews publishes departures. With appliedOnly, a resignation that
// still waits for its turn is left out, as the states show only teams that
// are out.
func departureViews(departures []models.Departure, appliedOnly bool) []models.DepartureView {
	views := []models.DepartureView{}
	for _, departure := range departures {
		if appliedOnly && departure.Turn == nil {
			continue
		}
		views = append(views, models.DepartureView{TeamColor: departure.Color, Kind: departure.Kind, Players: slices.Clone(departure.Players), TurnNumber: departure.Turn})
	}
	return views
}

// Resign records that the caller's team resigns the lobby's real-time game,
// and returns the caller's state. Like every game request it first ends an
// overdue turn. The team leaves when the open turn plays (playTurn): its
// castles turn neutral and keep their soldiers, its soldier groups are
// removed, and its commands of that turn are dropped. The turn no longer waits
// for the team, so when everyone else has completed it, as Go bots do at
// once, it plays in this request, and the state is the next generation's, or
// the outcome. Calling it again changes nothing. A game that waits for every
// player is refused with NotRealTimeProblemType, a finished game with
// GameEndedProblemType, and a team that is already out with
// TeamOutProblemType.
func (service *GenerationService) Resign(ctx context.Context, principal domain.Principal, lobbyID string) (models.GenerationState, error) {
	database := service.Database.WithContext(ctx)
	game, err := dal.FindLobbyGame(database, lobbyID)
	if err != nil {
		return models.GenerationState{}, err
	}
	observability.Annotate(ctx, slog.Uint64("game_id", uint64(game.ID)))
	if err = service.authorize(ctx, principal, game); err != nil {
		return models.GenerationState{}, err
	}
	// The pace is set at the start, so this needs no lock.
	if game.TurnPaceMilliseconds == nil {
		return models.GenerationState{}, &GenerationError{Status: 409, Type: NotRealTimeProblemType,
			Detail: "only a real-time game, one with a player who plays by hand, can be resigned; this game waits for every player's bot"}
	}
	if game.Outcome == nil {
		if _, err = service.playGoBotTurns(ctx, game.ID, goBotReadBudget); err != nil {
			return models.GenerationState{}, err
		}
	}
	var state models.GenerationState
	var end turnEnd
	var events []Publication
	var turns []turnToSend
	// A rejection after this request ended a turn still commits the end.
	var refused error
	goBotsOnly := false
	err = dal.WithLockedGame(database, game.ID, func(transaction *gorm.DB, locked *models.Game) error {
		players, err := dal.LoadFrozenPlayers(transaction, locked.ID)
		if err != nil {
			return err
		}
		player, err := frozenPlayer(players, principal.PlayerID)
		if err != nil {
			return err
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
		refuse := func(rejection error) error {
			if end.played || len(end.events) > 0 {
				refused = rejection
				return nil
			}
			return rejection
		}
		if locked.Outcome != nil {
			return refuse(&GenerationError{Status: 409, Detail: "the game has ended: " + describeOutcome(locked), Type: GameEndedProblemType})
		}
		departure := teamDeparture(locked, player.TeamColor)
		if departure != nil && departure.Turn != nil {
			return refuse(teamOut(locked, *departure))
		}
		if !slices.Contains(standingTeams(generation, locked.Teams), player.TeamColor) {
			return refuse(&GenerationError{Status: 409, Detail: "your team has been eliminated, so it has nothing to resign", Type: TeamOutProblemType})
		}
		if departure == nil {
			locked.Departures = append(locked.Departures, models.Departure{Color: player.TeamColor, Kind: models.DepartureResigned, Players: []string{player.DisplayName}})
			if err := dal.SaveGameDepartures(transaction, locked); err != nil {
				return err
			}
		}
		inputs, err := dal.LoadGenerationInputs(transaction, generation.ID)
		if err != nil {
			return err
		}
		// The turn no longer waits for the resigning team.
		if turnComplete(locked, generation, players, inputs) {
			if generation, inputs, events, turns, err = playTurn(ctx, transaction, locked, players, inputs, service.now(), service.Bots); err != nil {
				return err
			}
			goBotsOnly = locked.Outcome == nil && slices.ContainsFunc(players, isGoBot) && turnComplete(locked, generation, players, inputs)
		}
		memory, err := dal.FindBotMemory(transaction, locked.ID, player.PlayerID)
		if err != nil {
			return err
		}
		if state, err = generationState(locked, generation, players, player, inputs, memory); err != nil {
			return err
		}
		return countStoredLogLines(transaction, &state)
	})
	if err != nil {
		return models.GenerationState{}, err
	}
	service.afterTurnEnd(ctx, game.ID, end, goBotReadBudget)
	service.publish(ctx, events)
	service.Bots.send(ctx, database, turns)
	if refused != nil {
		return models.GenerationState{}, refused
	}
	if goBotsOnly {
		// The resignation has committed, so a failure here only stops the Go
		// bots until the next game request.
		if _, err := service.playGoBotTurns(ctx, game.ID, goBotReadBudget); err != nil {
			observability.RecordError(ctx, fmt.Errorf("play Go bot turns: %w", err))
		}
	}
	return state, nil
}

// EndAbandonedRound answers the lobby's round-ended hook: whether the game of
// a lobby round has ended, so that the lobby can be reopened. A game whose
// players all closed their tabs gets no game request, so the hook first ends
// it when nobody has played it for its turn limit at now (abandonGame), and
// pushes game.ended with publisher. It has the lobby's short hook timeout, so
// it plays no turn, has no Go bots play, and takes nobody out for silence. A
// round whose start bound no game counts as ended, so the lobby can't be
// stuck.
func EndAbandonedRound(ctx context.Context, database *gorm.DB, publisher Publisher, lobbyID string, round int64, now time.Time) (bool, error) {
	database = database.WithContext(ctx)
	game, err := dal.FindLobbyRoundGame(database, lobbyID, round)
	if err != nil || game == nil || game.Outcome != nil {
		return err == nil, err
	}
	// A look without the lock first, so that a running game's hook takes the
	// lock only once the game is abandoned.
	createdAt, found, err := dal.LatestGenerationCreatedAt(database, game.ID)
	if err != nil || !found || !abandoned(game, &models.Generation{Model: gorm.Model{CreatedAt: createdAt}}, now) {
		return false, err
	}
	var events []Publication
	err = dal.WithLockedGame(database, game.ID, func(transaction *gorm.DB, locked *models.Game) error {
		game = locked
		generation, err := dal.GetLatestGenerationForGame(transaction, locked.ID)
		if err != nil || !abandoned(locked, generation, now) {
			return err
		}
		players, err := dal.LoadFrozenPlayers(transaction, locked.ID)
		if err != nil {
			return err
		}
		events, err = abandonGame(transaction, locked, generation, players)
		return err
	})
	if err != nil {
		return false, err
	}
	service := GenerationService{Database: database, Publisher: publisher}
	service.publish(ctx, events)
	return game.Outcome != nil, nil
}
