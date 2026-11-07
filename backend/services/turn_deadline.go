package services

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"islandBattles/backend/dal"
	"islandBattles/backend/models"
	"islandBattles/backend/simulation"
	"islandBattles/lobby/realtime"

	"gorm.io/gorm"
)

// turnDeadline is when a generation's turn times out: its creation time plus
// the game's turn timeout. It is nil when the game has no turn timeout, and
// once the game has ended.
func turnDeadline(game *models.Game, generation *models.Generation) *time.Time {
	if game.Outcome != nil || game.TurnTimeoutSeconds == nil {
		return nil
	}
	deadline := generation.CreatedAt.Add(time.Duration(*game.TurnTimeoutSeconds) * time.Second)
	return &deadline
}

// now is the service's clock, which tests move past a deadline.
func (service *GenerationService) now() time.Time {
	if service.Now != nil {
		return service.Now()
	}
	return time.Now()
}

// expireTurn applies the deadline of the game's open turn, generation, under
// the game lock. The server runs no timers, so every game request calls it
// before anything else. Before the deadline, or once the game has ended, it
// does nothing. After it, every team still in the game with a player who
// hasn't completed the turn has timed out and loses; eliminated teams never
// do, and when only their players are late nothing happens. The game ends with
// simulation.TimeoutOutcome on generation's board, which stays the final
// generation. It returns the game.ended events to push once the transaction
// commits, or none when the game goes on.
func expireTurn(transaction *gorm.DB, game *models.Game, generation *models.Generation, players []models.FrozenGamePlayer, now time.Time) ([]realtime.Publication, error) {
	deadline := turnDeadline(game, generation)
	if deadline == nil || now.Before(*deadline) {
		return nil, nil
	}
	inputs, err := dal.LoadGenerationInputs(transaction, generation.ID)
	if err != nil {
		return nil, err
	}
	completed := map[string]bool{}
	for _, input := range inputs {
		completed[input.PlayerID] = input.Completed
	}
	board, err := dal.LoadGameState(transaction, game.ID)
	if err != nil {
		return nil, err
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
	// turn always waits for someone. Go bots complete a turn in the
	// transaction that creates it, and once only they are left a request plays
	// it later, so with them the turn is open and complete between requests,
	// and doesn't time out.
	if waiting == 0 && !slices.ContainsFunc(players, isGoBot) {
		return nil, fmt.Errorf("generation %d of game %d is open, but every roster player completed it", generation.ID, game.ID)
	}
	if len(late) == 0 {
		return nil, nil
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
	if err := dal.SaveGameOutcome(transaction, game, *outcome); err != nil {
		return nil, err
	}
	memories, err := dal.LoadBotMemories(transaction, game.ID)
	if err != nil {
		return nil, err
	}
	return generationEvents(game, generation, players, inputs, memories)
}

// applyOverdueDeadline has the Go bots play, as every game request does, and
// then applies the deadline of the game's open turn, for the reads that take
// no game lock: the replay, the game log and the demo. It checks the deadline
// without the lock first, so a running game's reads lock it only once its
// deadline has passed. It returns the game as it is afterwards.
func (service *GenerationService) applyOverdueDeadline(ctx context.Context, game *models.Game) (*models.Game, error) {
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
	if game.Outcome != nil || game.TurnTimeoutSeconds == nil {
		return game, nil
	}
	database := service.Database.WithContext(ctx)
	createdAt, found, err := dal.LatestGenerationCreatedAt(database, game.ID)
	if err != nil || !found || service.now().Before(*turnDeadline(game, &models.Generation{Model: gorm.Model{CreatedAt: createdAt}})) {
		return game, err
	}
	var events []realtime.Publication
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
		events, err = expireTurn(transaction, locked, generation, players, service.now())
		return err
	})
	if err != nil {
		return nil, err
	}
	service.publish(ctx, events)
	return game, nil
}

// publish pushes committed game events. A failed push is logged by the
// publisher and does not fail the request; clients recover over HTTP.
func (service *GenerationService) publish(ctx context.Context, events []realtime.Publication) {
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

// describeTimedOutTeam names a team that timed out and its players who didn't
// answer, such as "Red lost: Sam's bot didn't answer within 30 s".
func describeTimedOutTeam(game *models.Game, team models.TimedOutTeam) string {
	bots := make([]string, 0, len(team.Players))
	for _, player := range team.Players {
		bots = append(bots, player+"'s")
	}
	subject := "bot"
	if len(bots) > 1 {
		subject = "bots"
	}
	within := ""
	if game.TurnTimeoutSeconds != nil {
		within = " within " + describeTimeout(*game.TurnTimeoutSeconds)
	}
	return fmt.Sprintf("%s lost: %s %s didn't answer%s", teamName(game, team.Color), joinNames(bots), subject, within)
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
