package dal

import (
	"islandBattles/backend/models"
	"islandBattles/lobby/domain"
	"islandBattles/lobby/postgres"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// BindLobbyGame persists a game and its simulation colors from a snapshot that
// application.GameBinding has already authorized and validated. Color zero is neutral.
func BindLobbyGame(database *gorm.DB, snapshot domain.Snapshot) (models.BindingResult, error) {
	teams := make([]models.TeamBinding, len(snapshot.Teams))
	for index, team := range snapshot.Teams {
		teams[index] = models.TeamBinding{TeamID: team.ID, TeamOrder: team.Order, TeamColor: uint(team.Order)}
	}
	lobbyID := snapshot.Lobby.ID
	return postgres.BindOnce(database, "idx_games_lobby_id", func(transaction *gorm.DB) (*models.BindingResult, error) {
		return loadLobbyBinding(transaction, lobbyID, teams)
	}, func(transaction *gorm.DB) (models.BindingResult, error) {
		game := models.Game{LobbyID: &lobbyID, Generations: []models.Generation{}}
		if err := transaction.Create(&game).Error; err != nil {
			return models.BindingResult{}, err
		}
		records := make([]models.TeamBindingRecord, len(teams))
		for index, team := range teams {
			records[index] = models.TeamBindingRecord{GameID: game.ID, TeamID: team.TeamID, TeamOrder: team.TeamOrder, TeamColor: team.TeamColor}
		}
		if err := transaction.Create(&records).Error; err != nil {
			return models.BindingResult{}, err
		}
		return models.BindingResult{GameID: game.ID, LobbyID: lobbyID, Teams: teams}, nil
	})
}

func loadLobbyBinding(database *gorm.DB, lobbyID string, expected []models.TeamBinding) (*models.BindingResult, error) {
	var games []models.Game
	if err := database.Where(&models.Game{LobbyID: &lobbyID}).Limit(1).Find(&games).Error; err != nil {
		return nil, err
	}
	if len(games) == 0 {
		return nil, nil
	}
	game := games[0]
	var records []models.TeamBindingRecord
	if err := database.Where(&models.TeamBindingRecord{GameID: game.ID}).Order(clause.OrderByColumn{Column: clause.Column{Name: "team_order"}}).Find(&records).Error; err != nil {
		return nil, err
	}
	if len(records) != len(expected) {
		return nil, domain.ErrUnavailable
	}
	result := &models.BindingResult{GameID: game.ID, LobbyID: lobbyID, Teams: make([]models.TeamBinding, len(records))}
	for index, record := range records {
		result.Teams[index] = models.TeamBinding{TeamID: record.TeamID, TeamOrder: record.TeamOrder, TeamColor: record.TeamColor}
		if result.Teams[index] != expected[index] {
			return nil, domain.ErrUnavailable
		}
	}
	return result, nil
}
