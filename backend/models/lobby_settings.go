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
// snapshot. They hold exactly one map choice: a default map by its template
// ID, or a whole map file. They also set the turn timeout, the game's length,
// the turn pace of a real-time game and whether the game is rated.
type LobbySettings struct {
	MapTemplateID uint             `json:"map_template_id,omitempty" minimum:"1" doc:"Start on this default map, a map_template_id from GET /map/templates. Give this or map, not both."`
	Map           *MapTemplateView `json:"map,omitempty" doc:"Start on this map file. Give this or map_template_id, not both."`
	// TurnTimeoutSeconds is nil for no limit. ParseLobbySettings fills in
	// DefaultTurnTimeoutSeconds when the settings leave it out.
	TurnTimeoutSeconds *uint `json:"turn_timeout_seconds,omitempty" minimum:"5" maximum:"600" nullable:"true" default:"30" doc:"How long each turn lasts, in whole seconds from 5 to 600, counted from when its generation was created; 30 when left out. null means no limit, for example while debugging a bot. When a turn's deadline passes before every player has completed it, each team with a player who hadn't loses, and the game ends. In a real-time game, see turn_pace_milliseconds, no turn times out; instead a player the server hasn't heard from for this long, or for one turn plus 5 seconds when that is longer, by the end of a turn has left the game and their team is out, and a game that no request reaches for this long after its open turn's end is abandoned: the team with the most castles wins. null turns both rules off."`
	// MaxGenerations is always set by ParseLobbySettings, to
	// DefaultMaxGenerations when the settings leave it out.
	MaxGenerations int `json:"max_generations,omitempty" minimum:"50" maximum:"1200" default:"300" doc:"The most turns the game may last, a whole number from 50 to 1200; 300 when left out. The turn that creates generation max_generations ends the game, if nothing has ended it before, and the team with the most castles wins."`
	// TurnPaceMilliseconds is always set by ParseLobbySettings, to
	// DefaultTurnPaceMilliseconds when the settings leave it out.
	TurnPaceMilliseconds uint `json:"turn_pace_milliseconds,omitempty" minimum:"500" maximum:"5000" default:"1000" doc:"How long each turn lasts in a real-time game, in whole milliseconds from 500 to 5000; 1000 when left out. A game is real-time when a player on its roster plays by hand: each turn then ends this long after the previous one, and the server plays it with the commands that arrived, so a player who sent nothing passes. A game of bots only doesn't use it and waits for every player."`
	// Rated is false when the settings leave it out.
	Rated bool `json:"rated,omitempty" default:"false" doc:"Whether the game is rated, false when left out. A rated lobby starts only when every member is signed in and plays by hand, with one of their bot names or with a registered bot, no two teams belong to one account, every bot seat is a default bot level, each level once, or a registered bot, each once, and the bots that play away have rated games left today. The game then records the competitor of each team, and its end changes their Elo ratings."`
}

// The turn paces a lobby may set for a real-time game, in milliseconds, and
// the pace of a lobby that sets none.
const (
	MinTurnPaceMilliseconds     = 500
	MaxTurnPaceMilliseconds     = 5000
	DefaultTurnPaceMilliseconds = 1000
)

// Turn timeouts a lobby may set, in seconds, and the timeout of a lobby that
// sets none.
const (
	MinTurnTimeoutSeconds     = 5
	MaxTurnTimeoutSeconds     = 600
	DefaultTurnTimeoutSeconds = 30
)

// The game lengths a lobby may set, in generations, and the length of a lobby
// that sets none. The turn that creates the last generation ends the game.
const (
	MinMaxGenerations     = 50
	MaxMaxGenerations     = 1200
	DefaultMaxGenerations = 300
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
// fields, settings without a map choice or with both, an inline map that DecodeMapTemplate
// rejects, and a turn timeout that is neither null nor a whole number of
// seconds from MinTurnTimeoutSeconds to MaxTurnTimeoutSeconds, and a
// max_generations that is not a whole number from MinMaxGenerations to
// MaxMaxGenerations, and a turn_pace_milliseconds that is not a whole number
// from MinTurnPaceMilliseconds to MaxTurnPaceMilliseconds, and a rated that
// is not a JSON boolean. Settings without a turn timeout get
// DefaultTurnTimeoutSeconds, without max_generations DefaultMaxGenerations,
// without a pace DefaultTurnPaceMilliseconds, and without rated false.
// Whether a template
// exists, and whether a map fits the lobby's teams, are checked separately.
// Every rejection is a *LobbySettingsError.
func ParseLobbySettings(raw json.RawMessage) (LobbySettings, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil || fields == nil {
		return LobbySettings{}, &LobbySettingsError{Details: []*huma.ErrorDetail{{Location: LobbySettingsLocation, Message: "must be a JSON object"}}}
	}
	for _, field := range slices.Sorted(maps.Keys(fields)) {
		if field != "map_template_id" && field != "map" && field != "turn_timeout_seconds" && field != "max_generations" && field != "turn_pace_milliseconds" && field != "rated" {
			return LobbySettings{}, NewLobbySettingsError(field, "is not an Island Battles setting; the settings are map_template_id, map, turn_timeout_seconds, max_generations, turn_pace_milliseconds and rated")
		}
	}
	rawTemplateID, hasTemplateID := fields["map_template_id"]
	rawMap, hasMap := fields["map"]
	if hasTemplateID && hasMap {
		return LobbySettings{}, NewLobbySettingsError("map", "cannot be combined with map_template_id; choose a default map or a map file")
	}
	// Every game starts on a default map or a map file.
	if !hasTemplateID && !hasMap {
		return LobbySettings{}, &LobbySettingsError{Details: []*huma.ErrorDetail{{Location: LobbySettingsLocation,
			Message: "must choose a map: map_template_id for a default map from GET /map/templates, or map for a map file"}}}
	}
	timeout, err := parseTurnTimeout(fields)
	if err != nil {
		return LobbySettings{}, err
	}
	maxGenerations, err := parseMaxGenerations(fields)
	if err != nil {
		return LobbySettings{}, err
	}
	pace, err := parseTurnPace(fields)
	if err != nil {
		return LobbySettings{}, err
	}
	rated, err := parseRated(fields)
	if err != nil {
		return LobbySettings{}, err
	}
	settings := LobbySettings{TurnTimeoutSeconds: timeout, MaxGenerations: maxGenerations, TurnPaceMilliseconds: pace, Rated: rated}
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

// parseMaxGenerations reads max_generations from the settings' fields, and
// DefaultMaxGenerations when absent. Like the turn timeout, it must be a JSON
// whole number: 50.5 and "300" are rejected.
func parseMaxGenerations(fields map[string]json.RawMessage) (int, error) {
	raw, present := fields["max_generations"]
	if !present {
		return DefaultMaxGenerations, nil
	}
	var generations int
	if err := json.Unmarshal(raw, &generations); err != nil || generations < MinMaxGenerations || generations > MaxMaxGenerations {
		return 0, NewLobbySettingsError("max_generations", fmt.Sprintf("must be a whole number of turns from %d to %d", MinMaxGenerations, MaxMaxGenerations))
	}
	return generations, nil
}

// parseTurnPace reads turn_pace_milliseconds from the settings' fields, and
// DefaultTurnPaceMilliseconds when absent. It must be a JSON whole number:
// 1.5, "1000" and null are rejected.
func parseTurnPace(fields map[string]json.RawMessage) (uint, error) {
	raw, present := fields["turn_pace_milliseconds"]
	if !present {
		return DefaultTurnPaceMilliseconds, nil
	}
	var pace uint
	if err := json.Unmarshal(raw, &pace); err != nil || pace < MinTurnPaceMilliseconds || pace > MaxTurnPaceMilliseconds {
		return 0, NewLobbySettingsError("turn_pace_milliseconds", fmt.Sprintf("must be a whole number of milliseconds from %d to %d", MinTurnPaceMilliseconds, MaxTurnPaceMilliseconds))
	}
	return pace, nil
}

// parseRated reads rated from the settings' fields, and false when absent. It
// must be a JSON boolean: null, 1 and "true" are rejected.
func parseRated(fields map[string]json.RawMessage) (bool, error) {
	raw, present := fields["rated"]
	if !present {
		return false, nil
	}
	switch strings.TrimSpace(string(raw)) {
	case "true":
		return true, nil
	case "false":
		return false, nil
	}
	return false, NewLobbySettingsError("rated", "must be true or false")
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
