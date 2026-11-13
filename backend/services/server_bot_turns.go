package services

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"sync"
	"time"

	"islandBattles/backend/dal"
	"islandBattles/backend/models"
	"islandBattles/backend/submittoken"

	"gorm.io/gorm"
)

// The server sends each new turn of a hosted or uploaded bot to a bot runner
// (#225). A generation is created inside a transaction under the game lock:
// at the start (runLobbyGame), and when a turn is played (playTurn), which
// every completing batch and the Go bots' loop (playGoBotTurns) do. The code
// that creates it collects the turns to send, with sentTurns, and sends them
// once the transaction has committed, with TurnSender.send, so a rolled back
// generation sends nothing. Sending only queues an asynchronous invocation:
// the server never waits for a bot, which submits its own answer with the
// turn's submit token, as a hosted bot does. A bot that never answers loses
// at the turn's deadline, like any other.

// BotRunners starts the bot runners. Invoke queues one event for runner, and
// returns once it is queued, never waiting for the bot: botrunner.Lambda
// invokes the runner functions asynchronously, and botrunner.Local runs the
// same runners in this process for local development.
type BotRunners interface {
	Invoke(ctx context.Context, runner models.BotRunnerName, event models.BotRunnerEvent) error
}

// PackageLinks presigns the GET of an uploaded package's zip for a runner,
// which has no AWS permissions of its own. packagestore.Store is one.
type PackageLinks interface {
	PresignDownload(ctx context.Context, key string, expires time.Duration) (string, error)
}

// ServerCouldNotStartBotMessage is the game log line a player gets for a turn
// whose runner the server couldn't start, such as when Lambda throttled the
// invocation. The turn then waits for its deadline.
const ServerCouldNotStartBotMessage = "The server couldn't start your bot this turn."

// sendTimeout bounds sending one generation's turns, after the request that
// created it has its answer, so a client that goes away doesn't stop them.
const sendTimeout = 10 * time.Second

// TurnSender sends the turns of hosted and uploaded bots to the bot runners.
// A nil *TurnSender sends nothing: those bots then play only through the
// turn deadline, as before #225.
type TurnSender struct {
	// Runners starts the runners.
	Runners BotRunners
	// Packages presigns the uploaded packages' GETs. Nil fails every
	// uploaded bot's turn.
	Packages PackageLinks
	// Tokens issues the turns' submit tokens.
	Tokens *submittoken.Signer
	// SubmitURL is where the bots submit: the game's public URL plus
	// /generation/commands. Empty fails every turn.
	SubmitURL string
}

// turnToSend is one bot's turn of a new generation, collected inside the
// transaction that created it, and sent after it commits.
type turnToSend struct {
	gameID, generationID uint
	player               models.FrozenGamePlayer
	// delivery is empty when the server has no submit tokens to issue.
	delivery models.TurnDelivery
}

// sentTurns collects the turns that the server sends for the open turn,
// generation: one for each hosted or uploaded bot of a team still in the
// game that hasn't completed it. A finished game sends none. inputs are the
// turn's inputs so far, and memories every player's stored memory. Each
// delivery is the one GET …/turn-delivery would answer the player.
func (sender *TurnSender) sentTurns(game *models.Game, generation *models.Generation, players []models.FrozenGamePlayer, inputs []models.GenerationInput, memories map[string]models.BotMemory) ([]turnToSend, error) {
	if sender == nil || game.Outcome != nil {
		return nil, nil
	}
	completed := map[string]bool{}
	for _, input := range inputs {
		completed[input.PlayerID] = input.Completed
	}
	standing := standingTeams(generation, game.Teams)
	var turns []turnToSend
	for _, player := range players {
		if !sentByServer(player) || completed[player.PlayerID] || !slices.Contains(standing, player.TeamColor) {
			continue
		}
		turn := turnToSend{gameID: game.ID, generationID: generation.ID, player: player}
		if sender.Tokens != nil {
			var err error
			if turn.delivery, err = newTurnDelivery(game, generation, player, inputs, memories[player.PlayerID], sender.Tokens, sender.SubmitURL); err != nil {
				return nil, err
			}
		}
		turns = append(turns, turn)
	}
	return turns, nil
}

// sentByServer reports whether the server sends the player's turns to a
// runner: a hosted bot, or an uploaded bot whose package was frozen.
func sentByServer(player models.FrozenGamePlayer) bool {
	return player.BotKind == models.HostedBot || (player.BotKind == models.UploadedBot && player.BotPackageKey != nil)
}

// send starts a runner for each turn, all at once, once the transaction
// that created their generation has committed. A turn whose runner can't be
// started, because Lambda throttled it or answered with an error, is logged,
// and the player gets the error line ServerCouldNotStartBotMessage in that
// turn's game log; the turn then waits for its deadline. Nothing here fails
// the request that created the generation.
func (sender *TurnSender) send(ctx context.Context, database *gorm.DB, turns []turnToSend) {
	if sender == nil || len(turns) == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), sendTimeout)
	defer cancel()
	var wait sync.WaitGroup
	for _, turn := range turns {
		wait.Add(1)
		go func() {
			defer wait.Done()
			runner, event, err := sender.event(ctx, turn)
			if err == nil {
				err = sender.Runners.Invoke(ctx, runner, event)
			}
			if err == nil {
				return
			}
			slog.ErrorContext(ctx, "The server couldn't start a bot's turn", "game_id", turn.gameID,
				"generation_id", turn.generationID, "bot_kind", turn.player.BotKind, "error", err)
			if err := recordUnstartedTurn(ctx, database, turn); err != nil {
				slog.ErrorContext(ctx, "The server couldn't log a bot's unstarted turn", "game_id", turn.gameID, "error", err)
			}
		}()
	}
	wait.Wait()
}

// event is the runner and event of one turn: the native runner delivers a
// hosted bot's turn to its URL, and the runner of an uploaded package's
// language plays it from a presigned GET of its zip, within the owner's
// limits.
func (sender *TurnSender) event(ctx context.Context, turn turnToSend) (models.BotRunnerName, models.BotRunnerEvent, error) {
	if sender.Runners == nil {
		return "", models.BotRunnerEvent{}, errors.New("no bot runners are configured")
	}
	if turn.delivery.Submit.Token == "" || turn.delivery.Submit.URL == "" {
		return "", models.BotRunnerEvent{}, errors.New("no submit token secret or submit URL is configured")
	}
	event := models.BotRunnerEvent{Delivery: turn.delivery}
	player := turn.player
	if player.BotKind == models.HostedBot {
		if player.BotURL == nil {
			return "", models.BotRunnerEvent{}, errors.New("the hosted bot has no URL")
		}
		event.Hosted = &models.BotRunnerHosted{URL: *player.BotURL}
		return models.BotRunnerNative, event, nil
	}
	var runner models.BotRunnerName
	switch language := deref(player.BotPackageLanguage); language {
	case "python":
		runner = models.BotRunnerPython
	case "native":
		runner = models.BotRunnerNative
	default:
		return "", models.BotRunnerEvent{}, fmt.Errorf("no runner plays packages of language %q", language)
	}
	if sender.Packages == nil {
		return "", models.BotRunnerEvent{}, errors.New("no bot package store is configured")
	}
	link, err := sender.Packages.PresignDownload(ctx, *player.BotPackageKey, models.BotRunnerPackageExpiry)
	if err != nil {
		return "", models.BotRunnerEvent{}, err
	}
	event.Package = &models.BotRunnerPackage{ID: deref(player.BotPackageID), URL: link, SizeBytes: deref(player.BotPackageSizeBytes)}
	limits := models.DefaultBotRunnerLimits
	event.Limits = &limits
	return runner, event, nil
}

func deref[T any](value *T) T {
	var zero T
	if value == nil {
		return zero
	}
	return *value
}

// recordUnstartedTurn adds ServerCouldNotStartBotMessage to the player's game
// log of the turn, as an error line from GoBotLogger, unless the player's
// lines of that turn are already at their limit.
func recordUnstartedTurn(ctx context.Context, database *gorm.DB, turn turnToSend) error {
	generationID := turn.generationID
	return dal.WithLockedGame(database.WithContext(ctx), turn.gameID, func(transaction *gorm.DB, _ *models.Game) error {
		stored, err := dal.CountGenerationLogs(transaction, generationID, turn.player.PlayerID)
		if err != nil || stored >= models.MaxGenerationLogLines {
			return err
		}
		return appendGenerationLogs(transaction, turn.gameID, generationID, turn.player.PlayerID, []models.GenerationLogLine{
			{Level: models.GenerationLogError, Logger: GoBotLogger, Message: ServerCouldNotStartBotMessage}})
	})
}

// newTurnDelivery is the player's turn of the open generation as a bot that
// runs outside the backend gets it: the BotTurn, with the board as a Go bot
// sees it, where the team's castles show what the player's earlier batches
// this turn, among inputs, left unreserved, and a submit token for submitURL
// that submits exactly this turn.
func newTurnDelivery(game *models.Game, generation *models.Generation, player models.FrozenGamePlayer, inputs []models.GenerationInput, memory models.BotMemory, tokens *submittoken.Signer, submitURL string) (models.TurnDelivery, error) {
	turn := newGoBotTurn(game, generation, player.TeamColor, memory)
	var err error
	if turn.Generation, err = goBotGeneration(generation, player.TeamColor, inputs); err != nil {
		return models.TurnDelivery{}, err
	}
	expires := generation.CreatedAt.Add(submitTokenNoLimit)
	if turn.TurnDeadlineAt != nil {
		expires = turn.TurnDeadlineAt.Add(submitTokenGrace)
	}
	token, err := tokens.Issue(submittoken.Claims{GameID: game.ID, PlayerID: player.PlayerID, GenerationID: generation.ID, Expires: expires})
	if err != nil {
		return models.TurnDelivery{}, err
	}
	return models.TurnDelivery{Turn: turn, Submit: models.TurnSubmit{URL: submitURL, Token: token, DeadlineAt: turn.TurnDeadlineAt}}, nil
}
