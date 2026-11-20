package services

import (
	"encoding/json"
	"fmt"

	"islandBattles/backend/dal"
	"islandBattles/backend/models"

	"gorm.io/gorm"
)

// GameSettings are the Island Battles settings of a lobby's game, checked and
// ready to use.
type GameSettings struct {
	// Map is ready for models.BuildMap.
	Map models.MapTemplateView
	// TurnTimeoutSeconds is nil for no limit.
	TurnTimeoutSeconds *uint
	// MaxGenerations is the game's last generation.
	MaxGenerations int
	// TurnPaceMilliseconds is how long each turn of a real-time game lasts.
	// The start uses it only when a player plays by hand.
	TurnPaceMilliseconds uint
	// Rated makes the game rated: each team is played by a competitor.
	Rated bool
}

// LobbyGameSettings returns the game settings that a lobby's game settings
// choose: the map, ready for models.BuildMap, the turn timeout in seconds, or
// nil for no limit, the game's last generation, the turn pace and whether the
// game is rated.
// The settings must choose a map: a default map that exists, or a map file.
// The map must be made for exactly teamCount teams: team colours 1 to
// teamCount, in the lobby's team order. Absent settings choose no map. A
// rejected choice is a *models.LobbySettingsError; any other error means the
// database could not be read.
func LobbyGameSettings(database *gorm.DB, rawSettings json.RawMessage, teamCount int) (GameSettings, error) {
	if len(rawSettings) == 0 {
		rawSettings = json.RawMessage(`{}`)
	}
	settings, err := models.ParseLobbySettings(rawSettings)
	if err != nil {
		return GameSettings{}, err
	}
	chosen := GameSettings{TurnTimeoutSeconds: settings.TurnTimeoutSeconds, MaxGenerations: settings.MaxGenerations, TurnPaceMilliseconds: settings.TurnPaceMilliseconds, Rated: settings.Rated}
	if settings.Map != nil {
		if err := models.CheckMapTeams(*settings.Map, teamCount, "map"); err != nil {
			return GameSettings{}, err
		}
		chosen.Map = *settings.Map
		return chosen, nil
	}
	template, found, err := dal.FindMapTemplate(database, settings.MapTemplateID)
	if err != nil {
		return GameSettings{}, err
	}
	if !found {
		return GameSettings{}, models.NewLobbySettingsError("map_template_id", fmt.Sprintf("no default map has ID %d", settings.MapTemplateID))
	}
	view := template.View()
	if err := models.CheckMapTeams(view, teamCount, "map_template_id"); err != nil {
		return GameSettings{}, err
	}
	chosen.Map = view
	return chosen, nil
}
