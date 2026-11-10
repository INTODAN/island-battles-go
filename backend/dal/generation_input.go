package dal

import (
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"islandBattles/backend/models"
	"islandBattles/lobby/domain"
)

// FindLobbyGame returns the lobby's newest game, the one of its highest
// round. A lobby's older games can't be reached once the next one starts.
func FindLobbyGame(database *gorm.DB, lobbyID string) (*models.Game, error) {
	var games []models.Game
	if err := database.Where(&models.Game{LobbyID: &lobbyID}).Order(clause.OrderByColumn{Column: clause.Column{Name: "lobby_round"}, Desc: true}).Limit(1).Find(&games).Error; err != nil {
		return nil, err
	}
	if len(games) == 0 {
		return nil, domain.ErrNotFound
	}
	return &games[0], nil
}

// LobbyRoundEnded reports whether the game of a lobby round has an outcome.
// A round without a game, whose start failed before it bound one, counts as
// ended, so that its lobby can be reopened.
func LobbyRoundEnded(database *gorm.DB, lobbyID string, round int64) (bool, error) {
	var games []models.Game
	if err := database.Select("ID", "Outcome").Where(&models.Game{LobbyID: &lobbyID, LobbyRound: round}).Limit(1).Find(&games).Error; err != nil {
		return false, err
	}
	return len(games) == 0 || games[0].Outcome != nil, nil
}

// WithLockedGame is the serialization boundary for initialization, reads and turns.
func WithLockedGame(database *gorm.DB, gameID uint, action func(*gorm.DB, *models.Game) error) error {
	if gameID == 0 {
		return domain.ErrNotFound
	}
	return database.Transaction(func(transaction *gorm.DB) error {
		var games []models.Game
		if err := transaction.Clauses(clause.Locking{Strength: "UPDATE"}).Where(&models.Game{Model: gorm.Model{ID: gameID}}).Limit(1).Find(&games).Error; err != nil {
			return err
		}
		if len(games) == 0 {
			return domain.ErrNotFound
		}
		return action(transaction, &games[0])
	})
}

func LoadFrozenPlayers(database *gorm.DB, gameID uint) ([]models.FrozenGamePlayer, error) {
	var players []models.FrozenGamePlayer
	err := database.Where(&models.FrozenGamePlayer{GameID: gameID}).Order(clause.OrderByColumn{Column: clause.Column{Name: "player_id"}}).Find(&players).Error
	return players, err
}

// FindFrozenPlayer returns nil when the player is not on the game's roster.
func FindFrozenPlayer(database *gorm.DB, gameID uint, playerID string) (*models.FrozenGamePlayer, error) {
	// GORM drops zero-valued struct conditions, which would match any roster.
	if gameID == 0 || playerID == "" {
		return nil, nil
	}
	var players []models.FrozenGamePlayer
	if err := database.Where(&models.FrozenGamePlayer{GameID: gameID, PlayerID: playerID}).Limit(1).Find(&players).Error; err != nil {
		return nil, err
	}
	if len(players) == 0 {
		return nil, nil
	}
	return &players[0], nil
}
func StoreFrozenPlayers(database *gorm.DB, players []models.FrozenGamePlayer) error {
	return database.Create(&players).Error
}
func LoadGenerationInputs(database *gorm.DB, generationID uint) ([]models.GenerationInput, error) {
	var inputs []models.GenerationInput
	err := database.Where(&models.GenerationInput{GenerationID: generationID}).Order(clause.OrderByColumn{Column: clause.Column{Name: "player_id"}}).Find(&inputs).Error
	return inputs, err
}
func StoreGenerationInput(database *gorm.DB, input *models.GenerationInput) error {
	return database.Save(input).Error
}
func FindGenerationRequest(database *gorm.DB, gameID uint, playerID, requestID string) (*models.GenerationRequest, error) {
	var requests []models.GenerationRequest
	err := database.Where(&models.GenerationRequest{GameID: gameID, PlayerID: playerID, RequestID: requestID}).Limit(1).Find(&requests).Error
	if err != nil {
		return nil, err
	}
	if len(requests) == 0 {
		return nil, nil
	}
	return &requests[0], nil
}
func StoreGenerationRequest(database *gorm.DB, request *models.GenerationRequest) error {
	return database.Create(request).Error
}

// SaveGameStart stores the replay's teams and the turn timeout, nil for no
// limit, when the game is initialized.
func SaveGameStart(database *gorm.DB, game *models.Game, teams []models.GameTeam, turnTimeoutSeconds *uint) error {
	game.Teams, game.TurnTimeoutSeconds = teams, turnTimeoutSeconds
	return database.Model(game).Select("Teams", "TurnTimeoutSeconds").Updates(game).Error
}

// SaveGameOutcome records how the game ended. The replay shows it from then on.
func SaveGameOutcome(database *gorm.DB, game *models.Game, outcome models.GameOutcome) error {
	game.Outcome = &outcome
	return database.Model(game).Select("Outcome").Updates(game).Error
}

func FindGame(database *gorm.DB, gameID uint) (*models.Game, error) {
	if gameID == 0 {
		return nil, domain.ErrNotFound
	}
	var games []models.Game
	if err := database.Where(&models.Game{Model: gorm.Model{ID: gameID}}).Limit(1).Find(&games).Error; err != nil {
		return nil, err
	}
	if len(games) == 0 {
		return nil, domain.ErrNotFound
	}
	return &games[0], nil
}
