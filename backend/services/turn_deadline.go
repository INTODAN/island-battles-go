package services

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
	"time"

	"islandBattles/backend/dal"
	"islandBattles/backend/models"
	"islandBattles/backend/observability"
	"islandBattles/backend/simulation"

	"gorm.io/gorm"
)

// turnDeadline is when a generation's turn ends. In a real-time game, whose
// TurnPaceMilliseconds isn't nil, it is the generation's TurnDeadlineAt, which
// nextTurnDeadline chose when the turn before it played, or, for the first
// generation, its creation time, the game's start, plus the pace; the turn
// then plays with the commands that arrived. In a game that waits for every
// player it is the creation time plus the game's turn timeout, when the turn
// times out. It is nil when the game waits and has no turn timeout, and once
// the game has ended.
func turnDeadline(game *models.Game, generation *models.Generation) *time.Time {
	var deadline time.Time
	switch {
	case game.Outcome != nil:
		return nil
	case game.TurnPaceMilliseconds != nil && generation.TurnDeadlineAt != nil:
		deadline = *generation.TurnDeadlineAt
	case game.TurnPaceMilliseconds != nil:
		deadline = generation.CreatedAt.Add(pace(game))
	case game.TurnTimeoutSeconds != nil:
		deadline = generation.CreatedAt.Add(time.Duration(*game.TurnTimeoutSeconds) * time.Second)
	default:
		return nil
	}
	return &deadline
}

// pace is a real-time game's turn pace.
func pace(game *models.Game) time.Duration {
	return time.Duration(*game.TurnPaceMilliseconds) * time.Millisecond
}

// nextTurnDeadline is when the turn that follows open ends, when a request
// plays open's turn at now: nil in a game that waits for every player. In a
// real-time game it is open's deadline plus the pace, so that turns come one
// pace apart on average however late the requests that play them arrive (a
// turn that plays late leaves that much less time for the next one). A turn
// that plays early, once every player completed it, starts the next pace at
// now instead. The next turn always lasts at least half a pace from now
// (minimumTurnShare): when a turn plays so late that the previous deadline
// plus the pace is closer than that, or already past, the next turn ends half
// a pace after now, so that turns never play back to back with no time to
// give orders, and the schedule continues from there.
func nextTurnDeadline(game *models.Game, open *models.Generation, now time.Time) *time.Time {
	if game.TurnPaceMilliseconds == nil {
		return nil
	}
	start := now
	if deadline := turnDeadline(game, open); deadline != nil && deadline.Before(now) {
		start = *deadline
	}
	next := start.Add(pace(game))
	if floor := now.Add(pace(game) / minimumTurnShare); next.Before(floor) {
		next = floor
	}
	return &next
}

// minimumTurnShare divides the pace into the shortest time a real-time turn
// lasts from the moment its generation is created (nextTurnDeadline): half a
// pace.
const minimumTurnShare = 2

// turnEnd is what ending a turn whose time is up did, for the request that
// found it overdue: what to do once its transaction commits, and the open
// generation afterwards.
type turnEnd struct {
	// generation is the open generation after the end: the new one when a
	// real-time turn was played, otherwise the one the caller passed.
	generation *models.Generation
	// played is set when a real-time turn was played.
	played bool
	// events are the events to push once the transaction commits: the new
	// generation's, or the game.ended of a turn that timed out.
	events []Publication
	// turns are the hosted and uploaded bots' turns of the new generation,
	// or their game-ended deliveries when the game ended, to send once the
	// transaction commits.
	turns []turnToSend
	// goBotsOnly is set when the new turn is already complete and only Go
	// bots are left to play it, so they play on after the commit
	// (playGoBotTurns), as after a completing batch.
	goBotsOnly bool
}

// endTurn ends the game's open turn, generation, once its time is up, under
// the game lock. The server runs no timers, so every game request calls it
// before anything else. Before turnDeadline, or once the game has ended, it
// does nothing, and a request that raced the one that ended the turn finds
// the new generation not yet due and does nothing either.
//
// In a real-time game it first ends a game that nobody played for its turn
// limit (abandonGame), on its open generation. Otherwise it takes out the
// teams whose player the server hasn't heard from (leaveSilentTeams), never
// caller's, the roster player who made the request, or "" for a watcher's
// read. Then it plays the turn as playTurn does after the last completion:
// every player of a team still in the game who hasn't completed it is treated
// as complete, so the commands of their batches with complete false are
// played, a player who sent none passes, and the stored memories stay as they
// are. It plays one turn, however late the request is: the next turn ends one
// pace after this one's end, but at least half a pace after now
// (nextTurnDeadline), so a late request never plays several turns at once and
// a game that nobody calls stays where it is.
//
// In a game that waits for every player, the turn times out (expireTurn).
func endTurn(ctx context.Context, transaction *gorm.DB, game *models.Game, generation *models.Generation, players []models.FrozenGamePlayer, caller string, now time.Time, sender *TurnSender) (turnEnd, error) {
	deadline := turnDeadline(game, generation)
	if deadline == nil || now.Before(*deadline) {
		return turnEnd{generation: generation}, nil
	}
	if game.TurnPaceMilliseconds == nil {
		events, turns, err := expireTurn(transaction, game, generation, players, sender)
		return turnEnd{generation: generation, events: events, turns: turns}, err
	}
	if abandoned(game, generation, now) {
		events, turns, err := abandonGame(transaction, game, generation, players, sender)
		return turnEnd{generation: generation, events: events, turns: turns}, err
	}
	if leaveSilentTeams(game, generation, players, caller, *deadline) {
		// Every team still in the game went silent: nobody played it.
		events, turns, err := abandonGame(transaction, game, generation, players, sender)
		return turnEnd{generation: generation, events: events, turns: turns}, err
	}
	inputs, err := dal.LoadGenerationInputs(transaction, generation.ID)
	if err != nil {
		return turnEnd{}, err
	}
	end := turnEnd{played: true}
	if end.generation, inputs, end.events, end.turns, err = playTurn(ctx, transaction, game, generation, players, inputs, now, sender); err != nil {
		return turnEnd{}, err
	}
	end.goBotsOnly = game.Outcome == nil && slices.ContainsFunc(players, isGoBot) && turnComplete(game, end.generation, players, inputs)
	return end, nil
}

// afterTurnEnd does what a committed turnEnd leaves to do: push its events,
// send its bots' turns, and have the Go bots play on for at most goBotBudget,
// the caller's (goBotWriteBudget or goBotReadBudget), when they are the only
// ones left. The end has committed, so a failure of the Go bots only stops
// them until the next game request.
func (service *GenerationService) afterTurnEnd(ctx context.Context, gameID uint, end turnEnd, goBotBudget time.Duration) {
	service.publish(ctx, end.events)
	service.Bots.send(ctx, service.Database.WithContext(ctx), end.turns)
	if end.goBotsOnly {
		if _, err := service.playGoBotTurns(ctx, gameID, goBotBudget); err != nil {
			observability.RecordError(ctx, fmt.Errorf("play Go bot turns: %w", err))
		}
	}
}

// now is the service's clock, which tests move past a deadline.
func (service *GenerationService) now() time.Time {
	if service.Now != nil {
		return service.Now()
	}
	return time.Now()
}

// expireTurn times out the open turn, generation, of a game that waits for
// every player, once endTurn found its deadline passed. Every team still in
// the game with a player who hasn't completed the turn has timed out and
// loses; eliminated teams never do, and when only their players are late
// nothing happens. The game ends with simulation.TimeoutOutcome on
// generation's board, which stays the final generation. It returns the
// game.ended events to push, and the game-ended deliveries to send with
// sender, once the transaction commits, or none when the game goes on.
func expireTurn(transaction *gorm.DB, game *models.Game, generation *models.Generation, players []models.FrozenGamePlayer, sender *TurnSender) ([]Publication, []turnToSend, error) {
	inputs, err := dal.LoadGenerationInputs(transaction, generation.ID)
	if err != nil {
		return nil, nil, err
	}
	completed := map[string]bool{}
	for _, input := range inputs {
		completed[input.PlayerID] = input.Completed
	}
	board, err := dal.LoadGameState(transaction, game.ID)
	if err != nil {
		return nil, nil, err
	}
	// Only teams still in the game can time out. The players of an
	// eliminated team have nothing to play, and the turn doesn't wait for
	// them.
	standing := simulation.StandingTeams(board, game.Teams)
	waiting, late := 0, map[uint][]string{}
	for _, player := range players {
		if !completed[player.PlayerID] {
			waiting++
			if slices.Contains(standing, player.TeamColor) {
				late[player.TeamColor] = append(late[player.TeamColor], player.DisplayName)
			}
		}
	}
	// The last completion plays the turn at once, so without Go bots an open
	// turn always waits for someone. Only a game that waits for every player
	// gets here: a real-time game plays its turn whoever completed it. Go bots complete a turn in the
	// transaction that creates it, and once only they are left a request plays
	// it later, so with them the turn is open and complete between requests,
	// and doesn't time out.
	if waiting == 0 && !slices.ContainsFunc(players, isGoBot) {
		return nil, nil, fmt.Errorf("generation %d of game %d is open, but every roster player completed it", generation.ID, game.ID)
	}
	if len(late) == 0 {
		return nil, nil, nil
	}
	colors := slices.Sorted(maps.Keys(late))
	timedOut := make([]models.TimedOutTeam, 0, len(colors))
	for _, color := range colors {
		slices.Sort(late[color])
		timedOut = append(timedOut, models.TimedOutTeam{Color: color, Players: late[color]})
	}
	outcome := simulation.TimeoutOutcome(board, game.Teams, colors)
	outcome.TimedOut = timedOut
	for _, color := range standing {
		if !slices.Contains(colors, color) {
			outcome.Survivors = append(outcome.Survivors, color)
		}
	}
	slices.Sort(outcome.Survivors)
	if err := rateGame(transaction, game, players, outcome, generation.Number); err != nil {
		return nil, nil, err
	}
	if err := dal.SaveGameOutcome(transaction, game, *outcome); err != nil {
		return nil, nil, err
	}
	return gameEndedWork(transaction, game, generation, players, sender)
}

// applyOverdueDeadline has the Go bots play, as every game request does, and
// then ends the game's open turn once its time is up (endTurn), for the reads
// that take no game lock: the replay, the game log, the demo and the watch
// reads. caller is the reader's player ID, whom endTurn never takes out. It
// checks the deadline without the lock first, so a running game's reads lock
// it only once its deadline has passed. It returns the game as it is
// afterwards.
func (service *GenerationService) applyOverdueDeadline(ctx context.Context, game *models.Game, caller string) (*models.Game, error) {
	if game.Outcome != nil {
		return game, nil
	}
	// Go bots play first, so they never time out.
	played, err := service.playGoBotTurns(ctx, game.ID, goBotReadBudget)
	if err != nil {
		return nil, err
	}
	if played {
		if game, err = dal.FindGame(service.Database.WithContext(ctx), game.ID); err != nil {
			return nil, err
		}
	}
	if game.Outcome != nil || (game.TurnPaceMilliseconds == nil && game.TurnTimeoutSeconds == nil) {
		return game, nil
	}
	database := service.Database.WithContext(ctx)
	latest, found, err := dal.LatestGenerationTiming(database, game.ID)
	if err != nil || !found {
		return game, err
	}
	if deadline := turnDeadline(game, latest); deadline == nil || service.now().Before(*deadline) {
		return game, nil
	}
	var end turnEnd
	err = dal.WithLockedGame(database, game.ID, func(transaction *gorm.DB, locked *models.Game) error {
		game = locked
		players, err := dal.LoadFrozenPlayers(transaction, locked.ID)
		if err != nil {
			return err
		}
		generation, err := dal.GetLatestGenerationForGame(transaction, locked.ID)
		if err != nil {
			return err
		}
		end, err = endTurn(ctx, transaction, locked, generation, players, caller, service.now(), service.Bots)
		return err
	})
	if err != nil {
		return nil, err
	}
	service.afterTurnEnd(ctx, game.ID, end, goBotReadBudget)
	if end.goBotsOnly {
		// The Go bots played on after the end, so the game may have ended.
		return dal.FindGame(database, game.ID)
	}
	return game, nil
}

// publish pushes committed game events. A failed push is logged by the
// publisher and does not fail the request; clients recover over HTTP.
func (service *GenerationService) publish(ctx context.Context, events []Publication) {
	if len(events) > 0 && service.Publisher != nil {
		_ = service.Publisher.Publish(ctx, events...)
	}
}

// describeTimeout writes a turn timeout as the lobby's create form offers it,
// such as "30 s" or "2 min".
func describeTimeout(seconds uint) string {
	if seconds%60 == 0 {
		return fmt.Sprintf("%d min", seconds/60)
	}
	return fmt.Sprintf("%d s", seconds)
}

// describeDuration writes a time in whole or fractional seconds, or in whole
// minutes, as the create forms offer them, such as "30 s", "5.5 s" or
// "2 min".
func describeDuration(duration time.Duration) string {
	if duration%time.Second == 0 {
		return describeTimeout(uint(duration / time.Second))
	}
	return describePace(uint(duration / time.Millisecond))
}

// describePace writes a turn pace in seconds, as the create forms offer it,
// such as "1 s" or "0.5 s".
func describePace(milliseconds uint) string {
	return strconv.FormatFloat(float64(milliseconds)/1000, 'f', -1, 64) + " s"
}

// describeTimedOutTeam names a team that timed out and the player who didn't
// answer, such as "Red lost: Sam's bot didn't answer within 30 s". A team has
// one player.
func describeTimedOutTeam(game *models.Game, team models.TimedOutTeam) string {
	within := ""
	if game.TurnTimeoutSeconds != nil {
		within = " within " + describeTimeout(*game.TurnTimeoutSeconds)
	}
	return fmt.Sprintf("%s lost: %s's bot didn't answer%s", teamName(game, team.Color), strings.Join(team.Players, ""), within)
}

// joinNames lists names in prose: "A", "A and B", "A, B and C".
func joinNames(names []string) string {
	if len(names) < 2 {
		return strings.Join(names, "")
	}
	return strings.Join(names[:len(names)-1], ", ") + " and " + names[len(names)-1]
}

// teamName is the lobby name of the team with color, or "Team <color>".
func teamName(game *models.Game, color uint) string {
	for _, team := range game.Teams {
		if team.Color == color {
			return team.Name
		}
	}
	return fmt.Sprintf("Team %d", color)
}
