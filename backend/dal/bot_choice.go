package dal

import (
	"context"
	"fmt"

	"islandBattles/backend/models"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// botChoiceMembershipConstraint ties a bot choice to its lobby membership.
const botChoiceMembershipConstraint = "fk_island_lobby_bot_choices_membership"

// MigrateBotChoices creates the bot choice table, with a foreign key to the
// generic lobby's memberships that deletes each choice with its membership,
// which lobby retention deletes with the lobby. The lobby migrations must
// have run first. Running it again changes nothing.
func MigrateBotChoices(ctx context.Context, database *gorm.DB) error {
	database = database.WithContext(ctx)
	if err := database.AutoMigrate(&models.LobbyBotChoice{}); err != nil {
		return err
	}
	if database.Migrator().HasConstraint(&models.LobbyBotChoice{}, botChoiceMembershipConstraint) {
		return nil
	}
	// AutoMigrate can't declare a foreign key to a table that the lobby's
	// own migrations own, so the constraint is added here.
	err := database.Exec(fmt.Sprintf(`ALTER TABLE %s ADD CONSTRAINT %s FOREIGN KEY (lobby_id, player_id) REFERENCES lobby_memberships (lobby_id, player_id) ON DELETE CASCADE`,
		models.LobbyBotChoice{}.TableName(), botChoiceMembershipConstraint)).Error
	if err != nil {
		return fmt.Errorf("add the bot choice membership key: %w", err)
	}
	return nil
}

// SaveBotChoice stores a lobby member's bot choice, replacing their earlier
// one.
func SaveBotChoice(database *gorm.DB, choice models.LobbyBotChoice) error {
	return database.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "lobby_id"}, {Name: "player_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"kind", "level", "updated_at"}),
	}).Create(&choice).Error
}

// LoadBotChoices returns the bot choices of a lobby's members by player ID.
// Members without a choice are absent.
func LoadBotChoices(database *gorm.DB, lobbyID string) (map[string]models.LobbyBotChoice, error) {
	// GORM drops zero-valued struct conditions, which would match any lobby.
	if lobbyID == "" {
		return nil, nil
	}
	var choices []models.LobbyBotChoice
	if err := database.Where(&models.LobbyBotChoice{LobbyID: lobbyID}).Find(&choices).Error; err != nil {
		return nil, err
	}
	byPlayer := make(map[string]models.LobbyBotChoice, len(choices))
	for _, choice := range choices {
		byPlayer[choice.PlayerID] = choice
	}
	return byPlayer, nil
}
