package models

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/danielgtaylor/huma/v2"
)

// LobbySettings are the Island Battles settings a host chooses when creating a
// lobby, its game_settings object. The lobby hands them back in the started
// snapshot. They hold at most one map choice: a default map by its template
// ID, or a whole map file. With neither, the game starts on the generated map.
type LobbySettings struct {
	MapTemplateID uint             `json:"map_template_id,omitempty" minimum:"1" doc:"Start on this default map, a map_template_id from GET /map/templates. Cannot be combined with map."`
	Map           *MapTemplateView `json:"map,omitempty" doc:"Start on this map file. Cannot be combined with map_template_id."`
}

// LobbySettingsLocation is where the settings sit in a lobby create request.
// Every LobbySettingsError detail is located under it.
const LobbySettingsLocation = "game_settings"

// LobbySettingsError rejects lobby settings and locates every invalid field
// in the create request, such as game_settings.map.castles[2].x.
type LobbySettingsError struct {
	Details []*huma.ErrorDetail
}

func (err *LobbySettingsError) Error() string {
	problems := make([]string, len(err.Details))
	for index, detail := range err.Details {
		problems[index] = detail.Location + ": " + detail.Message
	}
	return strings.Join(problems, "; ")
}

// NewLobbySettingsError rejects one settings field, such as map_template_id.
func NewLobbySettingsError(field, message string) *LobbySettingsError {
	return &LobbySettingsError{Details: []*huma.ErrorDetail{{Location: LobbySettingsLocation + "." + field, Message: message}}}
}

// ParseLobbySettings decodes a lobby's game settings. It rejects unknown
// fields, both map choices at once, and an inline map that DecodeMapTemplate
// rejects. Whether a template exists, and whether a map fits the lobby's
// teams, are checked separately. Every rejection is a *LobbySettingsError.
func ParseLobbySettings(raw json.RawMessage) (LobbySettings, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil || fields == nil {
		return LobbySettings{}, &LobbySettingsError{Details: []*huma.ErrorDetail{{Location: LobbySettingsLocation, Message: "must be a JSON object"}}}
	}
	for _, field := range slices.Sorted(maps.Keys(fields)) {
		if field != "map_template_id" && field != "map" {
			return LobbySettings{}, NewLobbySettingsError(field, "is not an Island Battles setting; the settings are map_template_id or map")
		}
	}
	rawTemplateID, hasTemplateID := fields["map_template_id"]
	rawMap, hasMap := fields["map"]
	if hasTemplateID && hasMap {
		return LobbySettings{}, NewLobbySettingsError("map", "cannot be combined with map_template_id; choose a default map or a map file")
	}
	var settings LobbySettings
	if hasTemplateID {
		if err := json.Unmarshal(rawTemplateID, &settings.MapTemplateID); err != nil || settings.MapTemplateID == 0 {
			return LobbySettings{}, NewLobbySettingsError("map_template_id", "must be the positive integer ID of a default map")
		}
	}
	if hasMap {
		template, err := DecodeMapTemplate(rawMap)
		var invalidTemplate *MapTemplateError
		if errors.As(err, &invalidTemplate) {
			located := &LobbySettingsError{}
			for _, detail := range invalidTemplate.Located(LobbySettingsLocation + ".map") {
				located.Details = append(located.Details, detail.(*huma.ErrorDetail))
			}
			return LobbySettings{}, located
		}
		if err != nil {
			return LobbySettings{}, err
		}
		settings.Map = &template
	}
	return settings, nil
}

// MapTeamCount returns the number of teams a map is made for: its
// player_amount, when its team colours are exactly 1 to player_amount. Colour
// 0 is neutral. Otherwise the map fits no lobby, and ok is false.
func MapTeamCount(template MapTemplateView) (teams int, ok bool) {
	colours := map[uint]bool{}
	for _, castle := range template.Castles {
		if castle.TeamColor != NeutralTeamColor {
			colours[castle.TeamColor] = true
		}
	}
	for colour := uint(1); colour <= template.PlayerAmount; colour++ {
		if !colours[colour] {
			return 0, false
		}
	}
	return int(template.PlayerAmount), len(colours) == int(template.PlayerAmount)
}

// CheckMapTeams requires a map to be made for exactly teamCount teams, the
// lobby's teams in order: player_amount equal to teamCount, and team colours
// exactly 1 to teamCount. It returns nil or a *LobbySettingsError at field,
// which is "map" or "map_template_id".
func CheckMapTeams(template MapTemplateView, teamCount int, field string) error {
	if teams, ok := MapTeamCount(template); ok && teams == teamCount {
		return nil
	}
	colours := []uint{}
	for _, castle := range template.Castles {
		if castle.TeamColor != NeutralTeamColor && !slices.Contains(colours, castle.TeamColor) {
			colours = append(colours, castle.TeamColor)
		}
	}
	slices.Sort(colours)
	return NewLobbySettingsError(field, fmt.Sprintf("map %q has player_amount %d and team colours %v, but this lobby has %d teams, so it needs player_amount %d and team colours exactly 1 to %d (0 is neutral)",
		template.MapName, template.PlayerAmount, colours, teamCount, teamCount, teamCount))
}
