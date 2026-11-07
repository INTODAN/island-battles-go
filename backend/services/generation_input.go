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

	"islandBattles/backend/dal"
	"islandBattles/backend/models"
	"islandBattles/backend/observability"
	"islandBattles/lobby/domain"
	"islandBattles/lobby/realtime"

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

// Problem types of command rejections that clients map to their own errors.
// send_soldier_group and upgrade_castle share them.
const (
	CastleNotFoundProblemType       = "urn:island-battles:problem:castle-not-found"
	CastleNotOwnedProblemType       = "urn:island-battles:problem:castle-not-owned"
	UnknownUpgradeTypeProblemType   = "urn:island-battles:problem:unknown-upgrade-type"
	UpgradeMaxLevelProblemType      = "urn:island-battles:problem:upgrade-max-level"
	InsufficientSoldiersProblemType = "urn:island-battles:problem:insufficient-soldiers"
)

// MaxGenerations caps a game. Turns are not paced, so bots play as fast as round
// trips allow. The turn that creates the last generation ends the game, so no
// game grows past it.
const MaxGenerations = 300

// Publisher pushes committed game events to Centrifugo right after their
// transaction. realtime.Publisher retries briefly and logs what still fails;
// clients recover a missed event by re-reading their state over HTTP.
type Publisher interface {
	Publish(context.Context, ...realtime.Publication) error
}

type GenerationService struct {
	Database  *gorm.DB
	Publisher Publisher
	// Now is the clock that turn deadlines are applied by; nil is time.Now.
	Now func() time.Time
}

// Current returns the caller's state of the lobby's game, after applying an
// overdue turn deadline, which ends the game.
func (service *GenerationService) Current(ctx context.Context, principal domain.Principal, lobbyID string) (models.GenerationState, error) {
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
		if _, err = service.playGoBotTurns(ctx, game.ID); err != nil {
			return models.GenerationState{}, err
		}
	}
	var state models.GenerationState
	var events []realtime.Publication
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
		if events, err = expireTurn(transaction, locked, generation, players, service.now()); err != nil {
			return err
		}
		inputs, err := dal.LoadGenerationInputs(transaction, generation.ID)
		if err != nil {
			return err
		}
		memory, err := dal.FindBotMemory(transaction, locked.ID, player.PlayerID)
		if err != nil {
			return err
		}
		if state, err = generationState(locked, generation, player, inputs, memory); err != nil {
			return err
		}
		return countStoredLogLines(transaction, &state)
	})
	if err != nil {
		return models.GenerationState{}, err
	}
	service.publish(ctx, events)
	return state, nil
}

// ApplyDeadline applies the deadline of the lobby game's open turn once it
// has passed, and returns the caller's state, as Current does. Clients that
// wait for other players call it at the deadline, because the server runs no
// timers. Applying it again changes nothing.
func (service *GenerationService) ApplyDeadline(ctx context.Context, principal domain.Principal, lobbyID string) (models.GenerationState, error) {
	return service.Current(ctx, principal, lobbyID)
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
	database := service.Database.WithContext(ctx)
	game, err := dal.FindGame(database, request.GameID)
	if err != nil {
		return models.GenerationState{}, err
	}
	if err = service.authorize(ctx, principal, game); err != nil {
		return models.GenerationState{}, err
	}
	if game.Outcome == nil {
		if _, err = service.playGoBotTurns(ctx, game.ID); err != nil {
			return models.GenerationState{}, err
		}
	}
	var state models.GenerationState
	var events []realtime.Publication
	// A batch that comes after its turn's deadline is rejected, but the
	// transaction that applied the deadline still commits.
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
		if events, err = expireTurn(transaction, locked, generation, players, service.now()); err != nil {
			return err
		}
		// After this request ended the game, its rejection must not roll the
		// ending back.
		rejectAfterDeadline := func(rejection error) error {
			if len(events) > 0 {
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
			if state, err = generationState(locked, generation, player, inputs, memory); err != nil {
				return err
			}
			return countStoredLogLines(transaction, &state)
		}
		if locked.Outcome != nil {
			return rejectAfterDeadline(&GenerationError{Status: 409, Detail: "the game has ended: " + describeOutcome(locked), Type: GameEndedProblemType})
		}
		if generation.ID != request.GenerationID {
			return reject(409, "generation is no longer current")
		}
		input := models.GenerationInput{GenerationID: generation.ID, PlayerID: principal.PlayerID, Commands: []byte("[]")}
		for _, candidate := range inputs {
			if candidate.PlayerID == principal.PlayerID {
				input = candidate
				break
			}
		}
		if input.Completed {
			return reject(409, "player turn is already complete")
		}
		commands, err := validateCommands(generation, player.TeamColor, inputs, request.Commands)
		if err != nil {
			return err
		}
		if err := appendGenerationLogs(transaction, locked.ID, generation.ID, principal.PlayerID, request.Logs); err != nil {
			return err
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
			if generation, inputs, events, err = playTurn(ctx, transaction, locked, players, inputs); err != nil {
				return err
			}
			// When only Go bots are left to play, they play on after this
			// request's turn has committed.
			goBotsOnly = locked.Outcome == nil && slices.ContainsFunc(players, isGoBot) && turnComplete(locked, generation, players, inputs)
		}
		if state, err = generationState(locked, generation, player, inputs, memory); err != nil {
			return err
		}
		return countStoredLogLines(transaction, &state)
	})
	if err != nil {
		return models.GenerationState{}, err
	}
	// The turn, or the deadline, has committed.
	service.publish(ctx, events)
	if late != nil {
		return models.GenerationState{}, late
	}
	if goBotsOnly {
		// The batch is stored, so a failure here only stops the Go bots
		// until the next game request.
		if _, err := service.playGoBotTurns(ctx, game.ID); err != nil {
			observability.RecordError(ctx, fmt.Errorf("play Go bot turns: %w", err))
		}
	}
	return state, nil
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
	reservations := newTurnReservations(generation)
	for _, input := range inputs {
		var commands []models.CommandFields
		if err := json.Unmarshal(input.Commands, &commands); err != nil {
			return nil, err
		}
		for _, command := range commands {
			// %v rather than %w: a stored command that no longer fits is a
			// server fault, not a rejection of this batch.
			if err := reservations.reserveStored(command); err != nil {
				return nil, fmt.Errorf("persisted reservations do not fit the generation: %v", err)
			}
		}
	}
	commands := make([]models.CommandFields, 0, len(public))
	for _, command := range public {
		var internal models.CommandFields
		var err error
		switch command.CommandType {
		case models.SendSoldierGroup:
			internal, err = reservations.acceptSend(teamColor, command.Parameters)
		case models.UpgradeCastle:
			internal, err = reservations.acceptUpgrade(teamColor, command.Parameters)
		default:
			return nil, reject(403, "only send_soldier_group and upgrade_castle are player commands")
		}
		if err != nil {
			return nil, err
		}
		commands = append(commands, internal)
	}
	return commands, nil
}

// generationState builds a player's state. memory is the memory sent with
// the player's latest completed turn.
func generationState(game *models.Game, generation *models.Generation, player models.FrozenGamePlayer, inputs []models.GenerationInput, memory models.BotMemory) (models.GenerationState, error) {
	channel, err := domain.PlayerChannel(player.PlayerID)
	if err != nil {
		return models.GenerationState{}, err
	}
	state := models.GenerationState{GameID: game.ID, LobbyID: *game.LobbyID, GenerationID: generation.ID, PlayerID: player.PlayerID, TeamColor: player.TeamColor, Channel: channel,
		Generation: GenerationView(generation), Outcome: outcomeView(game), Memory: memory, TurnDeadlineAt: turnDeadline(game, generation)}
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
// ID.
func generationEvents(game *models.Game, generation *models.Generation, players []models.FrozenGamePlayer, inputs []models.GenerationInput, memories map[string]models.BotMemory) ([]realtime.Publication, error) {
	events := make([]realtime.Publication, 0, len(players))
	for _, player := range players {
		channel, err := domain.PlayerChannel(player.PlayerID)
		if err != nil {
			return nil, err
		}
		state, err := generationState(game, generation, player, inputs, memories[player.PlayerID])
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
		events = append(events, realtime.Publication{Channel: channel, Data: payload})
	}
	return events, nil
}
