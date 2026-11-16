package services

import (
	"encoding/json"
	"fmt"

	"islandBattles/backend/dal"
	"islandBattles/backend/models"

	"gorm.io/gorm"
)

// LobbyGameSettings returns the map that a lobby's game settings choose, ready
// for models.BuildMap, and their turn timeout in seconds, or nil for no limit.
// The settings must choose a map: a default map that exists, or a map file.
// The map must be made for exactly teamCount teams: team colours 1 to
// teamCount, in the lobby's team order. Absent settings choose no map. A
// rejected choice is a *models.LobbySettingsError; any other error means the
// database could not be read.
func LobbyGameSettings(database *gorm.DB, rawSettings json.RawMessage, teamCount int) (models.MapTemplateView, *uint, error) {
	if len(rawSettings) == 0 {
		rawSettings = json.RawMessage(`{}`)
	}
	settings, err := models.ParseLobbySettings(rawSettings)
	if err != nil {
		return models.MapTemplateView{}, nil, err
	}
	if settings.Map != nil {
		if err := models.CheckMapTeams(*settings.Map, teamCount, "map"); err != nil {
			return models.MapTemplateView{}, nil, err
		}
		return *settings.Map, settings.TurnTimeoutSeconds, nil
	}
	template, found, err := dal.FindMapTemplate(database, settings.MapTemplateID)
	if err != nil {
		return models.MapTemplateView{}, nil, err
	}
	if !found {
		return models.MapTemplateView{}, nil, models.NewLobbySettingsError("map_template_id", fmt.Sprintf("no default map has ID %d", settings.MapTemplateID))
	}
	view := template.View()
	if err := models.CheckMapTeams(view, teamCount, "map_template_id"); err != nil {
		return models.MapTemplateView{}, nil, err
	}
	return view, settings.TurnTimeoutSeconds, nil
}
