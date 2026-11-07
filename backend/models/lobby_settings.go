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
// They also set the turn timeout.
type LobbySettings struct {
	MapTemplateID uint             `json:"map_template_id,omitempty" minimum:"1" doc:"Start on this default map, a map_template_id from GET /map/templates. Cannot be combined with map."`
	Map           *MapTemplateView `json:"map,omitempty" doc:"Start on this map file. Cannot be combined with map_template_id."`
	// TurnTimeoutSeconds is nil for no limit. ParseLobbySettings fills in
	// DefaultTurnTimeoutSeconds when the settings leave it out.
	TurnTimeoutSeconds *uint `json:"turn_timeout_seconds,omitempty" minimum:"5" maximum:"600" nullable:"true" default:"30" doc:"How long each turn lasts, in whole seconds from 5 to 600, counted from when its generation was created; 30 when left out. null means no limit, for example while debugging a bot. When a turn's deadline passes before every player has completed it, each team with a player who hadn't loses, and the game ends."`
}

// Turn timeouts a lobby may set, in seconds, and the timeout of a lobby that
// sets none.
const (
	MinTurnTimeoutSeconds     = 5
	MaxTurnTimeoutSeconds     = 600
	DefaultTurnTimeoutSeconds = 30
)

// LobbySettingsLocation is where the settings sit in a lobby create request.
// Every LobbySettingsError detail is located under it.
const LobbySettingsLocation = "game_settings"

// LobbySettingsError rejects lobby settings and locates every invalid field
// in the create request, such as game_settings.map.castles[2].x.
type LobbySettingsError struct {
	Details []*huma.ErrorDetail
}

// MaxLobbySettingsProblems bounds the problems an error message lists, so a
// large invalid map can't produce a huge response. The rest are counted.
const MaxLobbySettingsProblems = 20

// Error lists the first MaxLobbySettingsProblems problems, each as its
// location and message, and counts any others.
func (err *LobbySettingsError) Error() string {
	listed := err.Details[:min(len(err.Details), MaxLobbySettingsProblems)]
	problems := make([]string, len(listed), len(listed)+1)
	for index, detail := range listed {
		problems[index] = detail.Location + ": " + detail.Message
	}
	if hidden := len(err.Details) - len(listed); hidden > 0 {
		problems = append(problems, fmt.Sprintf("and %d more problems", hidden))
	}
	return strings.Join(problems, "; ")
}

// NewLobbySettingsError rejects one settings field, such as map_template_id.
func NewLobbySettingsError(field, message string) *LobbySettingsError {
	return &LobbySettingsError{Details: []*huma.ErrorDetail{{Location: LobbySettingsLocation + "." + field, Message: message}}}
}

// ParseLobbySettings decodes a lobby's game settings. It rejects unknown
// fields, both map choices at once, an inline map that DecodeMapTemplate
// rejects, and a turn timeout that is neither null nor a whole number of
// seconds from MinTurnTimeoutSeconds to MaxTurnTimeoutSeconds. Settings
// without a turn timeout get DefaultTurnTimeoutSeconds. Whether a template
// exists, and whether a map fits the lobby's teams, are checked separately.
// Every rejection is a *LobbySettingsError.
func ParseLobbySettings(raw json.RawMessage) (LobbySettings, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil || fields == nil {
		return LobbySettings{}, &LobbySettingsError{Details: []*huma.ErrorDetail{{Location: LobbySettingsLocation, Message: "must be a JSON object"}}}
	}
	for _, field := range slices.Sorted(maps.Keys(fields)) {
		if field != "map_template_id" && field != "map" && field != "turn_timeout_seconds" {
			return LobbySettings{}, NewLobbySettingsError(field, "is not an Island Battles setting; the settings are map_template_id, map and turn_timeout_seconds")
		}
	}
	rawTemplateID, hasTemplateID := fields["map_template_id"]
	rawMap, hasMap := fields["map"]
	if hasTemplateID && hasMap {
		return LobbySettings{}, NewLobbySettingsError("map", "cannot be combined with map_template_id; choose a default map or a map file")
	}
	timeout, err := parseTurnTimeout(fields)
	if err != nil {
		return LobbySettings{}, err
	}
	settings := LobbySettings{TurnTimeoutSeconds: timeout}
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

// parseTurnTimeout reads turn_timeout_seconds from the settings' fields: nil
// for null, which means no limit, and DefaultTurnTimeoutSeconds when absent.
func parseTurnTimeout(fields map[string]json.RawMessage) (*uint, error) {
	raw, present := fields["turn_timeout_seconds"]
	if !present {
		timeout := uint(DefaultTurnTimeoutSeconds)
		return &timeout, nil
	}
	if strings.TrimSpace(string(raw)) == "null" {
		return nil, nil
	}
	var timeout uint
	if err := json.Unmarshal(raw, &timeout); err != nil || timeout < MinTurnTimeoutSeconds || timeout > MaxTurnTimeoutSeconds {
		return nil, NewLobbySettingsError("turn_timeout_seconds", fmt.Sprintf("must be a whole number of seconds from %d to %d, or null for no limit", MinTurnTimeoutSeconds, MaxTurnTimeoutSeconds))
	}
	return &timeout, nil
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
