package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"

	"islandBattles/backend/bots"
	"islandBattles/backend/dal"
	"islandBattles/backend/models"
	"islandBattles/backend/simulation"
	"islandBattles/lobby/realtime"

	"gorm.io/gorm"
)

// Go bots are the default bots of the bots package, which the backend plays
// itself for the roster players whose FrozenGamePlayer.BotLevel is set. A Go
// bot plays each generation's turn in the transaction that creates the
// generation, and completes it at once, so it never times out.

// goBotTimeBudget is how long one request keeps playing the turns of a game
// in which every player still in the game is a Go bot. The next game request
// continues where it stopped. A request plays at least one turn. Tests
// shorten it.
var goBotTimeBudget = 5 * time.Second

// goBotRequestMargin is how much of a request's own deadline, such as the
// Lambda's, playing Go bots leaves for the rest of the request.
const goBotRequestMargin = 2 * time.Second

// GoBotLogger names the game log line the backend writes for a Go bot whose
// turn failed.
const GoBotLogger = "island_battles.bots"

// isGoBot reports whether the backend plays the player.
func isGoBot(player models.FrozenGamePlayer) bool { return player.BotLevel != nil }

// goBotTurn is a Go bot's checked answer: the commands to store, as
// POST /generation/commands would store them, its log lines and its memory.
type goBotTurn struct {
	commands []models.CommandFields
	logs     []models.GenerationLogLine
	memory   models.BotMemory
}

// runGoBot plays a Go bot's turn and checks its result with the rules of a
// completing POST /generation/commands batch, against the commands already
// accepted this turn. An error means the bot failed: it returned an error,
// panicked, or answered with something the rules reject.
func runGoBot(ctx context.Context, level uint, turn models.BotTurn, generation *models.Generation, inputs []models.GenerationInput) (played goBotTurn, err error) {
	bot, found := bots.ForLevel(level)
	if !found {
		return goBotTurn{}, fmt.Errorf("there is no default bot of level %d", level)
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			played, err = goBotTurn{}, fmt.Errorf("the bot panicked: %v", recovered)
		}
	}()
	result, err := bot.Play(ctx, turn)
	if err != nil {
		return goBotTurn{}, err
	}
	if result.Commands == nil || len(result.Commands) > 1000 {
		return goBotTurn{}, fmt.Errorf("commands must be an array of at most 1000 commands")
	}
	if err := validateLogLines(result.Logs); err != nil {
		return goBotTurn{}, err
	}
	if played.memory, err = validateBotMemory(result.Memory); err != nil {
		return goBotTurn{}, err
	}
	if played.commands, err = validateCommands(generation, turn.TeamColor, inputs, result.Commands); err != nil {
		return goBotTurn{}, err
	}
	played.logs = result.Logs
	return played, nil
}

// standingTeams lists the colours of the teams that still have castles or
// soldier groups in a generation, as simulation.StandingTeams does on its
// board.
func standingTeams(generation *models.Generation, teams []models.GameTeam) []uint {
	present := map[uint]bool{}
	for _, castle := range generation.Map.Castles {
		present[castle.TeamColor] = true
	}
	for _, group := range generation.SoldierGroups {
		present[group.TeamColor] = true
	}
	standing := []uint{}
	for _, team := range teams {
		if present[team.Color] {
			standing = append(standing, team.Color)
		}
	}
	return standing
}

// playGoBots plays and completes the open turn, generation, for every Go bot
// of a team still in the game that hasn't completed it, in the caller's
// transaction, and returns the turn's inputs with theirs. inputs are the
// turn's inputs so far. Each bot gets the memory it returned last turn and
// sees the commands its teammates have already sent. Its commands, log lines
// and memory are stored as a completing batch would store them. A bot that
// fails passes the turn: it sends no commands, keeps its memory, and its game
// log gets one error line.
func playGoBots(ctx context.Context, transaction *gorm.DB, game *models.Game, generation *models.Generation, players []models.FrozenGamePlayer, inputs []models.GenerationInput) ([]models.GenerationInput, error) {
	completed := map[string]bool{}
	for _, input := range inputs {
		completed[input.PlayerID] = input.Completed
	}
	waiting := slices.DeleteFunc(slices.Clone(players), func(player models.FrozenGamePlayer) bool {
		return !isGoBot(player) || completed[player.PlayerID]
	})
	if len(waiting) == 0 || game.Outcome != nil {
		return inputs, nil
	}
	// Teammates' bots play in a fixed order, as the stored roster is read.
	slices.SortFunc(waiting, func(left, right models.FrozenGamePlayer) int { return strings.Compare(left.PlayerID, right.PlayerID) })
	standing := standingTeams(generation, game.Teams)
	memories, err := dal.LoadBotMemories(transaction, game.ID)
	if err != nil {
		return nil, err
	}
	for _, player := range waiting {
		if !slices.Contains(standing, player.TeamColor) {
			continue
		}
		turn := models.BotTurn{GameID: game.ID, Generation: GenerationView(generation), TeamColor: player.TeamColor,
			Memory: memories[player.PlayerID], TurnDeadlineAt: turnDeadline(game, generation)}
		played, err := runGoBot(ctx, *player.BotLevel, turn, generation, inputs)
		if err != nil {
			slog.WarnContext(ctx, "A Go bot failed its turn and passes it", "game_id", game.ID, "generation_id", generation.ID, "bot_level", *player.BotLevel, "error", err)
			played = goBotTurn{memory: memories[player.PlayerID], logs: []models.GenerationLogLine{{Level: models.GenerationLogError, Logger: GoBotLogger,
				Message: truncateLogMessage(fmt.Sprintf("The level %d bot failed this turn, so it passes: %v", *player.BotLevel, err))}}}
		}
		index := slices.IndexFunc(inputs, func(input models.GenerationInput) bool { return input.PlayerID == player.PlayerID })
		input := models.GenerationInput{GenerationID: generation.ID, PlayerID: player.PlayerID, Commands: []byte("[]")}
		if index >= 0 {
			input = inputs[index]
		}
		// The bot's lines follow any its player stored this turn. Lines that
		// would pass the limit with those are left out, rather than the turn.
		var rejected *GenerationError
		if err := appendGenerationLogs(transaction, game.ID, generation.ID, player.PlayerID, played.logs); err != nil && !errors.As(err, &rejected) {
			return nil, err
		}
		var accumulated []models.CommandFields
		if err := json.Unmarshal(input.Commands, &accumulated); err != nil {
			return nil, err
		}
		if input.Commands, err = json.Marshal(append(accumulated, played.commands...)); err != nil {
			return nil, err
		}
		input.Completed = true
		if err := dal.StoreGenerationInput(transaction, &input); err != nil {
			return nil, err
		}
		if err := dal.SaveBotMemory(transaction, &models.GamePlayerBotMemory{GameID: game.ID, PlayerID: player.PlayerID, GenerationID: generation.ID, Memory: played.memory}); err != nil {
			return nil, err
		}
		if index >= 0 {
			inputs[index] = input
		} else {
			inputs = append(inputs, input)
		}
	}
	return inputs, nil
}

// truncateLogMessage cuts a message to the length a game log line may have.
func truncateLogMessage(message string) string {
	runes := []rune(message)
	if len(runes) <= models.MaxGenerationLogMessageLength {
		return message
	}
	return string(runes[:models.MaxGenerationLogMessageLength-1]) + "…"
}

// turnComplete reports whether every roster player of a team still in the
// game has completed the open turn, generation, whose inputs are given.
// Players of eliminated teams have nothing to play, so the turn doesn't wait
// for them.
func turnComplete(game *models.Game, generation *models.Generation, players []models.FrozenGamePlayer, inputs []models.GenerationInput) bool {
	completed := map[string]bool{}
	for _, input := range inputs {
		completed[input.PlayerID] = input.Completed
	}
	standing := standingTeams(generation, game.Teams)
	for _, player := range players {
		if !completed[player.PlayerID] && slices.Contains(standing, player.TeamColor) {
			return false
		}
	}
	return len(players) > 0
}

// playTurn plays a completed turn: it applies every player's commands and the
// end-of-turn steps as the next generation, ends the game when the end rule
// says so, and has the Go bots play the new generation at once. It returns
// the new generation, its inputs, which are the Go bots', and the events to
// push once the transaction commits.
func playTurn(ctx context.Context, transaction *gorm.DB, game *models.Game, players []models.FrozenGamePlayer, inputs []models.GenerationInput) (*models.Generation, []models.GenerationInput, []realtime.Publication, error) {
	allCommands := []models.CommandFields{}
	for _, input := range inputs {
		var playerCommands []models.CommandFields
		if err := json.Unmarshal(input.Commands, &playerCommands); err != nil {
			return nil, nil, nil, err
		}
		allCommands = append(allCommands, playerCommands...)
	}
	generationID, board, err := migrateGeneration(transaction, game.ID, allCommands)
	if err != nil {
		return nil, nil, nil, err
	}
	generations, err := dal.CountGenerationsForGame(transaction, game.ID)
	if err != nil {
		return nil, nil, nil, err
	}
	// A game that ends keeps this generation as its last one.
	if outcome := simulation.Outcome(board, game.Teams, generations >= MaxGenerations); outcome != nil {
		if err := dal.SaveGameOutcome(transaction, game, *outcome); err != nil {
			return nil, nil, nil, err
		}
	}
	generation, err := dal.GetGeneration(transaction, generationID)
	if err != nil {
		return nil, nil, nil, err
	}
	inputs, err = playGoBots(ctx, transaction, game, generation, players, nil)
	if err != nil {
		return nil, nil, nil, err
	}
	memories, err := dal.LoadBotMemories(transaction, game.ID)
	if err != nil {
		return nil, nil, nil, err
	}
	events, err := generationEvents(game, generation, players, inputs, memories)
	if err != nil {
		return nil, nil, nil, err
	}
	return generation, inputs, events, nil
}

// playGoBotTurns brings a game's Go bots up to date, and returns whether it
// played a turn. Every game request calls it before its own work, and a
// request whose turn left only Go bots to play calls it after.
//
// It has every Go bot of a team still in the game play the open turn, if one
// hasn't yet, and plays the turn once every player still in the game has
// completed it. While the players still in the game are all Go bots, it keeps
// playing turns, each in its own transaction and pushed as it commits, until
// the game ends or goBotTimeBudget runs out. The next game request continues.
// A game without Go bots costs one read of its roster.
func (service *GenerationService) playGoBotTurns(ctx context.Context, gameID uint) (bool, error) {
	database := service.Database.WithContext(ctx)
	players, err := dal.LoadFrozenPlayers(database, gameID)
	if err != nil || !slices.ContainsFunc(players, isGoBot) {
		return false, err
	}
	stop := time.Now().Add(goBotTimeBudget)
	if deadline, ok := ctx.Deadline(); ok {
		if margin := deadline.Add(-goBotRequestMargin); margin.Before(stop) {
			stop = margin
		}
	}
	played := false
	for ctx.Err() == nil {
		var events []realtime.Publication
		next := false
		err := dal.WithLockedGame(database, gameID, func(transaction *gorm.DB, game *models.Game) error {
			// Teams are saved with the first generation.
			if game.Outcome != nil || len(game.Teams) == 0 {
				return nil
			}
			generation, err := dal.GetLatestGenerationForGame(transaction, game.ID)
			if err != nil {
				return err
			}
			inputs, err := dal.LoadGenerationInputs(transaction, generation.ID)
			if err != nil {
				return err
			}
			if inputs, err = playGoBots(ctx, transaction, game, generation, players, inputs); err != nil {
				return err
			}
			if !turnComplete(game, generation, players, inputs) {
				return nil
			}
			_, _, events, err = playTurn(ctx, transaction, game, players, inputs)
			next = game.Outcome == nil
			return err
		})
		if err != nil {
			return played, err
		}
		service.publish(ctx, events)
		played = played || len(events) > 0
		if !next || !time.Now().Before(stop) {
			break
		}
	}
	return played, nil
}
