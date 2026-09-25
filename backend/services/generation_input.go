package services

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"sort"

	"islandBattles/backend/dal"
	"islandBattles/backend/dal/commandHandlers"
	"islandBattles/backend/models"
	"islandBattles/lobby/domain"

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

type GenerationService struct {
	Database *gorm.DB
	Lobbies  domain.SnapshotReader
}

func (service *GenerationService) authorize(ctx context.Context, principal domain.Principal, game *models.Game) error {
	if game == nil || game.LobbyID == nil {
		return reject(404, "game not found")
	}
	snapshot, err := service.Lobbies.Snapshot(ctx, principal, domain.Scope{GameKey: "island-battles", LobbyID: *game.LobbyID})
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return reject(404, "game not found")
		}
		return err
	}
	if snapshot.Lobby.ID != *game.LobbyID || snapshot.Lobby.GameKey != "island-battles" {
		return reject(404, "game not found")
	}
	if snapshot.Lobby.Phase != domain.PhaseStarted {
		return reject(409, "lobby has not started")
	}
	for _, player := range snapshot.StartRoster {
		if player.PlayerID == principal.PlayerID {
			return nil
		}
	}
	return reject(404, "game not found")
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
		state = generationState(locked, generation, player, inputs)
		return nil
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
			state = generationState(locked, generation, player, inputs)
			return nil
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
			if err := queueGenerationNotifications(transaction, locked, generation, players); err != nil {
				return err
			}
			if err := appendPlayableReplay(transaction, locked, generation); err != nil {
				return err
			}
		}
		state = generationState(locked, generation, player, inputs)
		return nil
	})
	return state, err
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
		castles[castle.LogicalCastleID] = castle
		available[castle.LogicalCastleID] = castle.SoldierAmount
	}
	for _, input := range inputs {
		var commands []models.CommandFields
		if err := json.Unmarshal(input.Commands, &commands); err != nil {
			return nil, err
		}
		for _, command := range commands {
			var parameters commandHandlers.SendSoldierGroupParams
			if err := json.Unmarshal(command.Parameters, &parameters); err != nil {
				return nil, err
			}
			if parameters.SoldierAmount > available[parameters.OriginLogicalCastleID] {
				return nil, errors.New("persisted reservations exceed available soldiers")
			}
			available[parameters.OriginLogicalCastleID] -= parameters.SoldierAmount
		}
	}
	commands := make([]models.CommandFields, 0, len(public))
	for _, command := range public {
		if command.CommandType != models.SendSoldierGroup {
			return nil, reject(403, "only send_soldier_group is a player command")
		}
		var parameters struct {
			Origin      *uint `json:"origin_logical_castle_id"`
			Destination *uint `json:"destination_logical_castle_id"`
			Soldiers    *uint `json:"soldier_amount"`
		}
		decoder := json.NewDecoder(bytes.NewReader(command.Parameters))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&parameters); err != nil {
			return nil, reject(422, "send_soldier_group parameters must contain integer castle IDs and soldier_amount")
		}
		var trailing any
		if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
			return nil, reject(422, "invalid parameters")
		}
		if parameters.Origin == nil || parameters.Destination == nil || parameters.Soldiers == nil || *parameters.Soldiers == 0 {
			return nil, reject(422, "origin_logical_castle_id, destination_logical_castle_id and positive soldier_amount are required")
		}
		if *parameters.Origin == *parameters.Destination {
			return nil, reject(422, "origin and destination must differ")
		}
		origin, exists := castles[*parameters.Origin]
		if !exists {
			return nil, reject(422, "origin castle does not exist in this generation")
		}
		if _, exists := castles[*parameters.Destination]; !exists {
			return nil, reject(422, "destination castle does not exist in this generation")
		}
		if origin.TeamColor != teamColor {
			return nil, reject(403, "origin castle is not owned by your team")
		}
		if *parameters.Soldiers > available[*parameters.Origin] {
			return nil, reject(409, "insufficient soldiers after accepted team reservations")
		}
		available[*parameters.Origin] -= *parameters.Soldiers
		raw, err := json.Marshal(commandHandlers.SendSoldierGroupParams{OriginLogicalCastleID: *parameters.Origin, DestinationLogicalCastleID: *parameters.Destination, SoldierAmount: *parameters.Soldiers})
		if err != nil {
			return nil, err
		}
		commands = append(commands, models.CommandFields{CommandType: models.SendSoldierGroup, Parameters: raw})
	}
	return commands, nil
}

func generationState(game *models.Game, generation *models.Generation, player models.FrozenGamePlayer, inputs []models.GenerationInput) models.GenerationState {
	state := models.GenerationState{GameID: game.ID, LobbyID: *game.LobbyID, GenerationID: generation.ID, PlayerID: player.PlayerID, TeamColor: player.TeamColor, Channel: "player:" + player.PlayerID, Generation: models.GenerationView{Castles: []models.CastleView{}, SoldierGroups: []models.SoldierGroupView{}}}
	state.Generation.Clashes = generation.Clashes
	for _, input := range inputs {
		if input.PlayerID == player.PlayerID {
			state.Submitted = input.Completed
		}
	}
	for _, castle := range generation.Map.Castles {
		state.Generation.Castles = append(state.Generation.Castles, models.CastleView{ID: castle.LogicalCastleID, TeamColor: castle.TeamColor, Soldiers: castle.SoldierAmount, X: castle.Position.P.X, Y: castle.Position.P.Y})
	}
	for _, group := range generation.SoldierGroups {
		state.Generation.SoldierGroups = append(state.Generation.SoldierGroups, models.SoldierGroupView{ID: group.LogicalSoldierGroupID, TeamColor: group.TeamColor, Soldiers: group.SoldierAmount, X: group.Position.P.X, Y: group.Position.P.Y, OriginLogicalCastleID: group.OriginCastle.LogicalCastleID, DestinationLogicalCastleID: group.DestinationCastle.LogicalCastleID, ClashRadius: group.EffectiveClashRadius()})
	}
	sort.Slice(state.Generation.Castles, func(left, right int) bool {
		return state.Generation.Castles[left].ID < state.Generation.Castles[right].ID
	})
	sort.Slice(state.Generation.SoldierGroups, func(left, right int) bool {
		return state.Generation.SoldierGroups[left].ID < state.Generation.SoldierGroups[right].ID
	})
	return state
}

func queueGenerationNotifications(database *gorm.DB, game *models.Game, generation *models.Generation, players []models.FrozenGamePlayer) error {
	notifications := make([]models.GenerationNotification, 0, len(players))
	for _, player := range players {
		payload, err := json.Marshal(struct {
			SchemaVersion int                    `json:"schema_version"`
			Type          string                 `json:"type"`
			State         models.GenerationState `json:"state"`
		}{1, "game.next_generation", generationState(game, generation, player, nil)})
		if err != nil {
			return err
		}
		notifications = append(notifications, models.GenerationNotification{GameID: game.ID, GenerationID: generation.ID, PlayerID: player.PlayerID, Payload: payload})
	}
	return dal.StoreGenerationNotifications(database, notifications)
}
