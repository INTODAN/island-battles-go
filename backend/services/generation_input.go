package services

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"github.com/INTODAN/mpl/domain"
	"islandBattles/backend/dal"
	"islandBattles/backend/models"
	"islandBattles/backend/observability"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// GenerationError carries a public, non-sensitive command rejection.
type GenerationError struct {
	Status int
	Detail string
	// Type is the problem type of a rejection that clients tell apart from
	// others with the same status, such as GameEndedProblemType. Empty means
	// about:blank.
	Type string
}

func (err *GenerationError) Error() string   { return err.Detail }
func reject(status int, detail string) error { return &GenerationError{Status: status, Detail: detail} }

// GameEndedProblemType is the problem type of the 409 that rejects commands
// for a finished game.
const GameEndedProblemType = "urn:island-battles:problem:game-ended"

// StaleGenerationProblemType is the problem type of the 409 that rejects a
// new batch for a generation whose turn is over, so that a client tells a
// real-time turn that ended without its batch from other conflicts.
const StaleGenerationProblemType = "urn:island-battles:problem:stale-generation"

// NotRealTimeProblemType is the problem type of the 409 that refuses a
// resignation in a game that waits for every player: only a real-time game
// can be resigned.
const NotRealTimeProblemType = "urn:island-battles:problem:not-real-time"

// TeamOutProblemType is the problem type of the 409 that refuses a batch, a
// turn delivery or a resignation of a player whose team resigned or left a
// real-time game, or, for a resignation, was eliminated.
const TeamOutProblemType = "urn:island-battles:problem:team-out"

// Problem types of command rejections that clients map to their own errors.
// send_soldier_group, upgrade_castle and burst_castle share them.
const (
	CastleNotFoundProblemType       = "urn:island-battles:problem:castle-not-found"
	CastleNotOwnedProblemType       = "urn:island-battles:problem:castle-not-owned"
	UnknownUpgradeTypeProblemType   = "urn:island-battles:problem:unknown-upgrade-type"
	UpgradeMaxLevelProblemType      = "urn:island-battles:problem:upgrade-max-level"
	InsufficientSoldiersProblemType = "urn:island-battles:problem:insufficient-soldiers"
	// BurstUnavailableProblemType rejects a burst of a castle that has burst
	// debt or a burst already accepted this turn.
	BurstUnavailableProblemType = "urn:island-battles:problem:burst-unavailable"
	// BurstAbovePotentialProblemType rejects a burst above the castle's
	// burst potential.
	BurstAbovePotentialProblemType = "urn:island-battles:problem:burst-above-potential"
)

// Publisher pushes committed game events to Centrifugo right after their
// transaction. The host's publisher retries briefly and logs what still
// fails; clients recover a missed event by re-reading their state over HTTP.
type Publisher interface {
	Publish(context.Context, ...Publication) error
}

// Publication is one event for one Centrifugo channel: a player's channel or
// a lobby's watch channel.
type Publication struct {
	Channel string
	Data    []byte
}

type GenerationService struct {
	Database  *gorm.DB
	Publisher Publisher
	// Now is the clock that turn deadlines are applied by; nil is time.Now.
	Now func() time.Time
	// Watchers authorizes the watcher reads, Watch and WatchDemo, against
	// the lobby's watcher rows. Nil lets only roster players read them.
	Watchers domain.WatchAuthorizer
	// Bots sends the turns of hosted and uploaded bots that requests
	// create. Nil sends none, and those bots play only through the deadline.
	Bots *TurnSender
}

// Current returns the caller's state of the lobby's game, after ending an
// overdue turn (endTurn): a real-time game plays it, and in a game that waits
// for every player it times out, which ends the game. Go bots catch up for at
// most goBotReadBudget first.
func (service *GenerationService) Current(ctx context.Context, principal domain.Principal, lobbyID string) (models.GenerationState, error) {
	return service.current(ctx, principal, lobbyID, goBotReadBudget)
}

// current is Current, with the time Go bots may play for first, and again
// after a real-time turn it played left only them.
func (service *GenerationService) current(ctx context.Context, principal domain.Principal, lobbyID string, goBotBudget time.Duration) (models.GenerationState, error) {
	database := service.Database.WithContext(ctx)
	game, err := dal.FindLobbyGame(database, lobbyID)
	if err != nil {
		return models.GenerationState{}, err
	}
	observability.Annotate(ctx, slog.Uint64("game_id", uint64(game.ID)))
	if err = service.authorize(ctx, principal, game); err != nil {
		return models.GenerationState{}, err
	}
	if game.Outcome == nil {
		if _, err = service.playGoBotTurns(ctx, game.ID, goBotBudget); err != nil {
			return models.GenerationState{}, err
		}
	}
	var state models.GenerationState
	var end turnEnd
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
		inputs, err := dal.LoadGenerationInputs(transaction, generation.ID)
		if err != nil {
			return err
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
	service.afterTurnEnd(ctx, game.ID, end, goBotBudget)
	return state, nil
}

// ApplyDeadline ends the lobby game's open turn once its time is up, and
// returns the caller's state, as Current does: in a real-time game it plays
// the turn, and in a game that waits for every player the turn times out.
// Clients call it at the turn's end, because the server runs no timers.
// Calling it before, or again after, changes nothing. Go bots catch up for at
// most goBotWriteBudget first.
func (service *GenerationService) ApplyDeadline(ctx context.Context, principal domain.Principal, lobbyID string) (models.GenerationState, error) {
	return service.current(ctx, principal, lobbyID, goBotWriteBudget)
}

func frozenPlayer(players []models.FrozenGamePlayer, playerID string) (models.FrozenGamePlayer, error) {
	for _, player := range players {
		if player.PlayerID == playerID {
			return player, nil
		}
	}
	return models.FrozenGamePlayer{}, reject(404, "game not found")
}

func (service *GenerationService) Register(ctx context.Context, principal domain.Principal, request models.RegisterGenerationCommands) (models.GenerationState, error) {
	if request.GameID == 0 || request.GenerationID == 0 {
		return models.GenerationState{}, reject(422, "game_id and generation_id must be positive")
	}
	// The game comes first, so that a batch refused for its body still tells
	// the server that its player is there (markSeen).
	database := service.Database.WithContext(ctx)
	game, err := dal.FindGame(database, request.GameID)
	if err != nil {
		return models.GenerationState{}, err
	}
	if err = service.authorize(ctx, principal, game); err != nil {
		return models.GenerationState{}, err
	}
	identifier, err := uuid.Parse(request.RequestID)
	if err != nil {
		return models.GenerationState{}, reject(422, "request_id must be a UUID")
	}
	request.RequestID = identifier.String()
	if request.Commands == nil || len(request.Commands) > 1000 {
		return models.GenerationState{}, reject(422, "commands must be an array of at most 1000 commands")
	}
	if err := validateLogLines(request.Logs); err != nil {
		return models.GenerationState{}, err
	}
	if request.Memory, err = validateBotMemory(request.Memory); err != nil {
		return models.GenerationState{}, err
	}
	complete := request.Complete == nil || *request.Complete
	request.Complete = &complete
	payloadHash, err := commandPayloadHash(request)
	if err != nil {
		return models.GenerationState{}, reject(422, "invalid command parameters")
	}
	if game.Outcome == nil {
		if _, err = service.playGoBotTurns(ctx, game.ID, goBotWriteBudget); err != nil {
			return models.GenerationState{}, err
		}
	}
	var state models.GenerationState
	var events []Publication
	var turns []turnToSend
	// end is what ending an overdue turn did before the batch.
	var end turnEnd
	// A batch that comes after its turn's end is rejected, but the
	// transaction that ended the turn still commits.
	var late error
	// goBotsOnly is set when this batch's turn left a turn that only Go bots
	// play, and has already completed.
	goBotsOnly := false
	err = dal.WithLockedGame(database, request.GameID, func(transaction *gorm.DB, locked *models.Game) error {
		players, err := dal.LoadFrozenPlayers(transaction, locked.ID)
		if err != nil {
			return err
		}
		player, err := frozenPlayer(players, principal.PlayerID)
		if err != nil {
			return err
		}
		previous, err := dal.FindGenerationRequest(transaction, request.GameID, principal.PlayerID, request.RequestID)
		if err != nil {
			return err
		}
		generation, err := dal.GetLatestGenerationForGame(transaction, locked.ID)
		if err != nil {
			return err
		}
		// The overdue turn ends first: a real-time game plays it, and a game
		// that waits for every player times out. Then the batch is judged.
		if end, err = endTurn(ctx, transaction, locked, generation, players, principal.PlayerID, service.now(), service.Bots); err != nil {
			return err
		}
		generation = end.generation
		// After this request ended the turn, its rejection must not roll the
		// end back.
		rejectAfterDeadline := func(rejection error) error {
			if end.played || len(end.events) > 0 {
				late = rejection
				return nil
			}
			return rejection
		}
		inputs, err := dal.LoadGenerationInputs(transaction, generation.ID)
		if err != nil {
			return err
		}
		if previous != nil {
			if previous.PayloadHash != payloadHash {
				return rejectAfterDeadline(reject(409, "request_id was already used for a different payload"))
			}
			memory, err := dal.FindBotMemory(transaction, locked.ID, player.PlayerID)
			if err != nil {
				return err
			}
			if state, err = generationState(locked, generation, players, player, inputs, memory); err != nil {
				return err
			}
			return countStoredLogLines(transaction, &state)
		}
		if locked.Outcome != nil {
			return rejectAfterDeadline(&GenerationError{Status: 409, Detail: "the game has ended: " + describeOutcome(locked), Type: GameEndedProblemType})
		}
		// A turn that this request ended is over for its batch, whichever
		// generation the batch names. In a real-time game a batch that
		// names one of the turns just before the open one joins the open
		// turn instead (lateBatchJoins); an older one is stale.
		lateBatch := end.played || generation.ID != request.GenerationID
		if lateBatch {
			joins, err := lateBatchJoins(transaction, locked, request.GenerationID)
			if err != nil {
				return err
			}
			if !joins {
				return rejectAfterDeadline(staleGeneration(locked))
			}
		}
		// A team that resigned or left plays no more commands.
		if departure := teamDeparture(locked, player.TeamColor); departure != nil {
			return rejectAfterDeadline(teamOut(locked, *departure))
		}
		input := models.GenerationInput{GenerationID: generation.ID, PlayerID: principal.PlayerID, Commands: []byte("[]")}
		for _, candidate := range inputs {
			if candidate.PlayerID == principal.PlayerID {
				input = candidate
				break
			}
		}
		if input.Completed {
			if lateBatch {
				// The player has already completed the open turn, so a late
				// batch can't join it: its turn passed without it.
				return rejectAfterDeadline(staleGeneration(locked))
			}
			return reject(409, "player turn is already complete")
		}
		// A late batch is checked against the open turn's board and the
		// commands it already holds, as any batch is.
		commands, err := validateCommands(generation, player.TeamColor, inputs, request.Commands)
		if err != nil {
			return rejectAfterDeadline(err)
		}
		// The lines stay with the turn they were written for, the one the
		// batch names, whose count the client keeps.
		if err := appendGenerationLogs(transaction, locked.ID, request.GenerationID, principal.PlayerID, request.Logs); err != nil {
			return rejectAfterDeadline(err)
		}
		var accumulated []models.CommandFields
		if err := json.Unmarshal(input.Commands, &accumulated); err != nil {
			return err
		}
		accumulated = append(accumulated, commands...)
		input.Commands, err = json.Marshal(accumulated)
		if err != nil {
			return err
		}
		// A late batch that completes its turn completes the open one: the
		// client answered one turn late, and its next batch is for the turn
		// after the open one.
		input.Completed = complete
		if err := dal.StoreGenerationInput(transaction, &input); err != nil {
			return err
		}
		// Only the completing batch saves memory, so the stored memory is
		// always the one sent with the player's latest completed turn.
		memory := request.Memory
		if complete {
			if err := dal.SaveBotMemory(transaction, &models.GamePlayerBotMemory{GameID: locked.ID, PlayerID: player.PlayerID, GenerationID: generation.ID, Memory: memory}); err != nil {
				return err
			}
		} else if memory, err = dal.FindBotMemory(transaction, locked.ID, player.PlayerID); err != nil {
			return err
		}
		ledger := models.GenerationRequest{GameID: locked.ID, PlayerID: principal.PlayerID, RequestID: request.RequestID, GenerationID: generation.ID, PayloadHash: payloadHash}
		if err := dal.StoreGenerationRequest(transaction, &ledger); err != nil {
			return err
		}
		inputs, err = dal.LoadGenerationInputs(transaction, generation.ID)
		if err != nil {
			return err
		}
		if turnComplete(locked, generation, players, inputs) {
			if generation, inputs, events, turns, err = playTurn(ctx, transaction, locked, generation, players, inputs, service.now(), service.Bots); err != nil {
				return err
			}
			// When only Go bots are left to play, they play on after this
			// request's turn has committed.
			goBotsOnly = locked.Outcome == nil && slices.ContainsFunc(players, isGoBot) && turnComplete(locked, generation, players, inputs)
		}
		if state, err = generationState(locked, generation, players, player, inputs, memory); err != nil {
			return err
		}
		return countStoredLogLines(transaction, &state)
	})
	if err != nil {
		return models.GenerationState{}, err
	}
	// The ended turn, and the batch's own turn, have committed.
	service.afterTurnEnd(ctx, game.ID, end, goBotWriteBudget)
	service.publish(ctx, events)
	service.Bots.send(ctx, database, turns)
	if late != nil {
		return models.GenerationState{}, late
	}
	if goBotsOnly {
		// The batch is stored, so a failure here only stops the Go bots
		// until the next game request.
		if _, err := service.playGoBotTurns(ctx, game.ID, goBotWriteBudget); err != nil {
			observability.RecordError(ctx, fmt.Errorf("play Go bot turns: %w", err))
		}
	}
	return state, nil
}

// LateTurns is how many turns before the open one a batch of a real-time game
// may name and still be played: a batch that arrives after its turn has
// played, naming one of these turns, joins the open turn (lateBatchJoins).
const LateTurns = 2

// lateBatchJoins reports whether a batch that names generationID, a
// generation whose turn has already played, joins the open turn: in a
// real-time game, when generationID is one of the LateTurns generations
// before the open one. Its commands are then checked against the open turn's
// board as any batch's are, and a batch that completes its turn completes the
// open one. A game that waits for every player plays a turn only once each
// player completed it, so there a batch for an earlier turn is always stale.
func lateBatchJoins(transaction *gorm.DB, game *models.Game, generationID uint) (bool, error) {
	if game.TurnPaceMilliseconds == nil {
		return false, nil
	}
	ids, err := dal.RecentGenerationIDs(transaction, game.ID, LateTurns+1)
	if err != nil || len(ids) < 2 {
		return false, err
	}
	return slices.Contains(ids[1:], generationID), nil
}

// staleGeneration rejects a batch for a generation whose turn is over, and
// that can't join the open turn (lateBatchJoins). In a real-time game its
// detail says how late a batch may be, so a bot's author sees why a slow bot
// passed.
func staleGeneration(game *models.Game) error {
	detail := "generation is no longer current"
	if game.TurnPaceMilliseconds != nil {
		detail += fmt.Sprintf(": in this real-time game each turn ends %s after the previous one and plays with the commands that arrived by then, and a batch that arrives later joins the open turn only when it names one of the %d turns before it and the player hasn't completed the open turn, so this batch came too late and was not played", describePace(*game.TurnPaceMilliseconds), LateTurns)
	}
	return &GenerationError{Status: 409, Detail: detail, Type: StaleGenerationProblemType}
}

func commandPayloadHash(request models.RegisterGenerationCommands) (string, error) {
	raw, err := json.Marshal(request)
	if err != nil {
		return "", err
	}
	var value any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		return "", err
	}
	canonical, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	hash := sha256.Sum256(canonical)
	return hex.EncodeToString(hash[:]), nil
}

// validateCommands checks a batch against the generation and every command
// already accepted this turn, by any player, and translates it into internal
// commands. A batch it accepts cannot make simulation.Simulate fail.
func validateCommands(generation *models.Generation, teamColor uint, inputs []models.GenerationInput, public []models.PublicGenerationCommand) ([]models.CommandFields, error) {
	reservations, err := reservedTurn(generation, inputs)
	if err != nil {
		return nil, err
	}
	// The batch's bursts are checked after its other commands, so a burst's
	// potential counts every upgrade of its batch, wherever the burst stands
	// in it, and of the turn's earlier batches. The commands keep their order.
	commands := make([]models.CommandFields, len(public))
	for _, bursts := range []bool{false, true} {
		for index, command := range public {
			if (command.CommandType == models.BurstCastle) != bursts {
				continue
			}
			var internal models.CommandFields
			switch command.CommandType {
			case models.SendSoldierGroup:
				internal, err = reservations.acceptSend(teamColor, command.Parameters)
			case models.UpgradeCastle:
				internal, err = reservations.acceptUpgrade(teamColor, command.Parameters)
			case models.BurstCastle:
				internal, err = reservations.acceptBurst(teamColor, command.Parameters)
			default:
				return nil, reject(403, "only send_soldier_group, upgrade_castle and burst_castle are player commands")
			}
			if err != nil {
				return nil, err
			}
			commands[index] = internal
		}
	}
	return commands, nil
}

// generationState builds a player's state, one of players. memory is the
// memory sent with the player's latest completed turn.
func generationState(game *models.Game, generation *models.Generation, players []models.FrozenGamePlayer, player models.FrozenGamePlayer, inputs []models.GenerationInput, memory models.BotMemory) (models.GenerationState, error) {
	if err := requireNumber(generation); err != nil {
		return models.GenerationState{}, err
	}
	channel, err := domain.PlayerChannel(player.PlayerID)
	if err != nil {
		return models.GenerationState{}, err
	}
	state := models.GenerationState{GameID: game.ID, LobbyID: *game.LobbyID, LobbyRound: game.LobbyRound, GenerationID: generation.ID, PlayerID: player.PlayerID, TeamColor: player.TeamColor, Channel: channel,
		Generation: GenerationView(generation), TurnNumber: generation.Number, MaxGenerations: game.MaxGenerations, Outcome: outcomeView(game), Memory: memory, TurnDeadlineAt: turnDeadline(game, generation), TurnPaceMilliseconds: game.TurnPaceMilliseconds, Rated: game.Rated, Bot: models.FrozenBot(player),
		RelayedBots: models.RelayedBots(players, player.PlayerID), Departures: departureViews(game.Departures, true)}
	for _, input := range inputs {
		if input.PlayerID == player.PlayerID {
			state.Submitted = input.Completed
		}
	}
	return state, nil
}

// generationEvents builds each roster player's event for a generation, for
// pushing to their personal channel once the transaction commits: a new
// generation, whose inputs are nil, or the open one that a turn deadline
// ended, with its inputs. The generation that ends the game is pushed as
// game.ended instead. memories holds each player's stored memory by player
// ID. The lobby's watch channel gets the same event with the watchers'
// WatchState, in the same push.
func generationEvents(game *models.Game, generation *models.Generation, players []models.FrozenGamePlayer, inputs []models.GenerationInput, memories map[string]models.BotMemory) ([]Publication, error) {
	events := make([]Publication, 0, len(players)+1)
	if game.LobbyID != nil {
		watch, err := watchEvent(game, generation)
		if err != nil {
			return nil, err
		}
		events = append(events, watch)
	}
	for _, player := range players {
		channel, err := domain.PlayerChannel(player.PlayerID)
		if err != nil {
			return nil, err
		}
		state, err := generationState(game, generation, players, player, inputs, memories[player.PlayerID])
		if err != nil {
			return nil, err
		}
		var event any = models.GenerationEvent{SchemaVersion: models.GenerationEventSchemaVersion, Type: models.GenerationEventType, State: state}
		if state.Outcome != nil {
			event = models.GameEndedEvent{SchemaVersion: models.GenerationEventSchemaVersion, Type: models.GameEndedEventType, State: state}
		}
		payload, err := json.Marshal(event)
		if err != nil {
			return nil, err
		}
		events = append(events, Publication{Channel: channel, Data: payload})
	}
	return events, nil
}
