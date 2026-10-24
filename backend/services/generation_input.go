package services

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"islandBattles/backend/dal"
	"islandBattles/backend/models"
	"islandBattles/backend/simulation"
	"islandBattles/lobby/domain"
	"islandBattles/lobby/realtime"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// GenerationError carries a public, non-sensitive command rejection.
type GenerationError struct {
	Status int
	Detail string
}

func (err *GenerationError) Error() string   { return err.Detail }
func reject(status int, detail string) error { return &GenerationError{Status: status, Detail: detail} }

// MaxGenerations caps a game. Turns are not paced, so bots play as fast as round
// trips allow; commands for the last generation are rejected, so no game grows
// past it.
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
}

func (service *GenerationService) Current(ctx context.Context, principal domain.Principal, lobbyID string) (models.GenerationState, error) {
	database := service.Database.WithContext(ctx)
	game, err := dal.FindLobbyGame(database, lobbyID)
	if err != nil {
		return models.GenerationState{}, err
	}
	if err = service.authorize(ctx, principal, game); err != nil {
		return models.GenerationState{}, err
	}
	var state models.GenerationState
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
		inputs, err := dal.LoadGenerationInputs(transaction, generation.ID)
		if err != nil {
			return err
		}
		state, err = generationState(locked, generation, player, inputs)
		return err
	})
	return state, err
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
	var state models.GenerationState
	var events []realtime.Publication
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
		inputs, err := dal.LoadGenerationInputs(transaction, generation.ID)
		if err != nil {
			return err
		}
		if previous != nil {
			if previous.PayloadHash != payloadHash {
				return reject(409, "request_id was already used for a different payload")
			}
			state, err = generationState(locked, generation, player, inputs)
			return err
		}
		if generation.ID != request.GenerationID {
			return reject(409, "generation is no longer current")
		}
		generations, err := dal.CountGenerationsForGame(transaction, locked.ID)
		if err != nil {
			return err
		}
		if generations >= MaxGenerations {
			return reject(409, fmt.Sprintf("game reached its limit of %d generations", MaxGenerations))
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
		ledger := models.GenerationRequest{GameID: locked.ID, PlayerID: principal.PlayerID, RequestID: request.RequestID, GenerationID: generation.ID, PayloadHash: payloadHash}
		if err := dal.StoreGenerationRequest(transaction, &ledger); err != nil {
			return err
		}
		inputs, err = dal.LoadGenerationInputs(transaction, generation.ID)
		if err != nil {
			return err
		}
		completed := map[string]bool{}
		for _, candidate := range inputs {
			completed[candidate.PlayerID] = candidate.Completed
		}
		ready := len(players) > 0
		for _, participant := range players {
			ready = ready && completed[participant.PlayerID]
		}
		if ready {
			allCommands := []models.CommandFields{}
			for _, candidate := range inputs {
				var playerCommands []models.CommandFields
				if err := json.Unmarshal(candidate.Commands, &playerCommands); err != nil {
					return err
				}
				allCommands = append(allCommands, playerCommands...)
			}
			generationID, err := MigrateGeneration(transaction, locked.ID, allCommands)
			if err != nil {
				return err
			}
			generation, err = dal.GetGeneration(transaction, generationID)
			if err != nil {
				return err
			}
			inputs = nil
			if events, err = generationEvents(locked, generation, players); err != nil {
				return err
			}
		}
		state, err = generationState(locked, generation, player, inputs)
		return err
	})
	if err != nil {
		return models.GenerationState{}, err
	}
	if len(events) > 0 {
		// The turn has committed. A failed push is logged and does not fail the request.
		_ = service.Publisher.Publish(ctx, events...)
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

func validateCommands(generation *models.Generation, teamColor uint, inputs []models.GenerationInput, public []models.PublicGenerationCommand) ([]models.CommandFields, error) {
	available := map[uint]uint{}
	castles := map[uint]models.Castle{}
	for _, castle := range generation.Map.Castles {
		castles[castle.PersistentCastleID] = castle
		available[castle.PersistentCastleID] = castle.SoldierAmount
	}
	for _, input := range inputs {
		var commands []models.CommandFields
		if err := json.Unmarshal(input.Commands, &commands); err != nil {
			return nil, err
		}
		for _, command := range commands {
			var parameters simulation.SendSoldierGroupParams
			if err := json.Unmarshal(command.Parameters, &parameters); err != nil {
				return nil, err
			}
			if parameters.SoldierAmount > available[parameters.OriginPersistentCastleID] {
				return nil, errors.New("persisted reservations exceed available soldiers")
			}
			available[parameters.OriginPersistentCastleID] -= parameters.SoldierAmount
		}
	}
	commands := make([]models.CommandFields, 0, len(public))
	for _, command := range public {
		if command.CommandType != models.SendSoldierGroup {
			return nil, reject(403, "only send_soldier_group is a player command")
		}
		var parameters models.SendSoldierGroupParameters
		decoder := json.NewDecoder(bytes.NewReader(command.Parameters))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&parameters); err != nil {
			return nil, reject(422, "send_soldier_group parameters must contain integer castle IDs and soldier_amount")
		}
		var trailing any
		if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
			return nil, reject(422, "invalid parameters")
		}
		if parameters.OriginCastleID == nil || parameters.DestinationCastleID == nil || parameters.SoldierAmount == nil || *parameters.SoldierAmount == 0 {
			return nil, reject(422, "origin_castle_id, destination_castle_id and positive soldier_amount are required")
		}
		if *parameters.OriginCastleID == *parameters.DestinationCastleID {
			return nil, reject(422, "origin and destination must differ")
		}
		origin, exists := castles[*parameters.OriginCastleID]
		if !exists {
			return nil, reject(422, "origin castle does not exist in this generation")
		}
		if _, exists := castles[*parameters.DestinationCastleID]; !exists {
			return nil, reject(422, "destination castle does not exist in this generation")
		}
		if origin.TeamColor != teamColor {
			return nil, reject(403, "origin castle is not owned by your team")
		}
		if *parameters.SoldierAmount > available[*parameters.OriginCastleID] {
			return nil, reject(409, "insufficient soldiers after accepted team reservations")
		}
		available[*parameters.OriginCastleID] -= *parameters.SoldierAmount
		raw, err := json.Marshal(simulation.SendSoldierGroupParams{OriginPersistentCastleID: *parameters.OriginCastleID, DestinationPersistentCastleID: *parameters.DestinationCastleID, SoldierAmount: *parameters.SoldierAmount})
		if err != nil {
			return nil, err
		}
		commands = append(commands, models.CommandFields{CommandType: models.SendSoldierGroup, Parameters: raw})
	}
	return commands, nil
}

func generationState(game *models.Game, generation *models.Generation, player models.FrozenGamePlayer, inputs []models.GenerationInput) (models.GenerationState, error) {
	channel, err := domain.PlayerChannel(player.PlayerID)
	if err != nil {
		return models.GenerationState{}, err
	}
	state := models.GenerationState{GameID: game.ID, LobbyID: *game.LobbyID, GenerationID: generation.ID, PlayerID: player.PlayerID, TeamColor: player.TeamColor, Channel: channel,
		Generation: generationView(generation)}
	for _, input := range inputs {
		if input.PlayerID == player.PlayerID {
			state.Submitted = input.Completed
		}
	}
	return state, nil
}

// generationEvents builds each roster player's event for a new generation, for
// pushing to their personal channel once the transaction commits.
func generationEvents(game *models.Game, generation *models.Generation, players []models.FrozenGamePlayer) ([]realtime.Publication, error) {
	events := make([]realtime.Publication, 0, len(players))
	for _, player := range players {
		channel, err := domain.PlayerChannel(player.PlayerID)
		if err != nil {
			return nil, err
		}
		state, err := generationState(game, generation, player, nil)
		if err != nil {
			return nil, err
		}
		payload, err := json.Marshal(models.GenerationEvent{SchemaVersion: models.GenerationEventSchemaVersion, Type: models.GenerationEventType, State: state})
		if err != nil {
			return nil, err
		}
		events = append(events, realtime.Publication{Channel: channel, Data: payload})
	}
	return events, nil
}
