package services

import (
	"encoding/json"
	"fmt"

	"islandBattles/backend/dal"
	"islandBattles/backend/models"

	"gorm.io/gorm"
)

// LobbyMap returns the map that a lobby's game settings choose, ready for
// models.BuildMap, or nil when they choose none, or are absent, and the game
// starts on the generated map. The chosen default map must exist, and the map must be made
// for exactly teamCount teams: team colours 1 to teamCount, in the lobby's team
// order. A rejected choice is a *models.LobbySettingsError; any other error
// means the database could not be read.
func LobbyMap(database *gorm.DB, rawSettings json.RawMessage, teamCount int) (*models.MapTemplateView, error) {
	if len(rawSettings) == 0 {
		return nil, nil
	}
	settings, err := models.ParseLobbySettings(rawSettings)
	if err != nil {
		return nil, err
	}
	switch {
	case settings.Map != nil:
		if err := models.CheckMapTeams(*settings.Map, teamCount, "map"); err != nil {
			return nil, err
		}
		return settings.Map, nil
	case settings.MapTemplateID != 0:
		template, found, err := dal.FindMapTemplate(database, settings.MapTemplateID)
		if err != nil {
			return nil, err
		}
		if !found {
			return nil, models.NewLobbySettingsError("map_template_id", fmt.Sprintf("no default map has ID %d", settings.MapTemplateID))
		}
		view := template.View()
		if err := models.CheckMapTeams(view, teamCount, "map_template_id"); err != nil {
			return nil, err
		}
		return &view, nil
	default:
		return nil, nil
	}
}
