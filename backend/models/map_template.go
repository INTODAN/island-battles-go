package models

import (
	"cmp"
	"encoding/json"
	"fmt"
	"maps"
	"math"
	"reflect"
	"slices"
	"strings"
	"sync"

	"github.com/danielgtaylor/huma/v2"
	"github.com/jackc/pgx/v5/pgtype"
	"islandBattles/lobby/domain"
	"islandBattles/lobby/httpapi"
)

// MapTemplateView is the map file format, published as MapTemplate. The same
// JSON creates a default map through POST /map/create and describes a map
// anywhere else one is exchanged. Check it with ValidateMapTemplate, then build
// a game map from it with BuildMap.
type MapTemplateView struct {
	MapFields
	Castles []CastleTemplateView `json:"castles" minItems:"1" nullable:"false" doc:"Each castle's position in the list is its castle ID in every game built from the map."`
}

// CastleTemplateView is one castle of a map file, published as CastleTemplate.
type CastleTemplateView struct {
	TeamColor             uint     `json:"team_color" doc:"Owning team colour, from 1 to player_amount; 0 is neutral."`
	X                     float64  `json:"x" doc:"Distance from the centre of the plane, between -plane_size_x/2 and plane_size_x/2."`
	Y                     float64  `json:"y" doc:"Distance from the centre of the plane, between -plane_size_y/2 and plane_size_y/2."`
	StartingSoldierAmount *uint    `json:"starting_soldier_amount,omitempty" nullable:"false" doc:"Soldiers in the castle when a game starts. Defaults to neutral_starting_soldier_amount."`
	Upgrades              Upgrades `json:"upgrades,omitempty" doc:"Starting level of each upgrade type. Omitted types start at level 1."`
}

// MapTemplateError rejects a map template and lists its invalid fields: the
// first MaxMapTemplateProblems of them, then one detail that counts the rest.
type MapTemplateError struct {
	// Details locate each problem relative to the template, such as castles[2].x.
	// A problem with the whole template has an empty location.
	Details []*huma.ErrorDetail
}

func (err *MapTemplateError) Error() string {
	problems := make([]string, len(err.Details))
	for index, detail := range err.Details {
		problems[index] = strings.TrimSpace(detail.Location + " " + detail.Message)
	}
	return "invalid map template: " + strings.Join(problems, "; ")
}

// Located places the details under location, such as "body" for a request
// body, ready to pass to a Huma error response.
func (err *MapTemplateError) Located(location string) []error {
	located := make([]error, len(err.Details))
	for index, detail := range err.Details {
		at := location
		if detail.Location != "" {
			at += "." + detail.Location
		}
		located[index] = &huma.ErrorDetail{Message: detail.Message, Location: at, Value: detail.Value}
	}
	return located
}

// mapFileSchema is the JSON Schema of the map file format, built from the same
// struct tags as the published MapTemplate schema.
var mapFileSchema = sync.OnceValues(func() (huma.Registry, *huma.Schema) {
	registry := huma.NewMapRegistry("#/components/schemas/", huma.DefaultSchemaNamer)
	return registry, registry.Schema(reflect.TypeFor[MapTemplateView](), false, "")
})

// DecodeMapTemplate strictly decodes a map file and validates it, so the
// result is ready for BuildMap. Unlike encoding/json, it rejects unknown keys
// and missing required fields instead of ignoring them or reading them as
// zero, checking the JSON against the same schema that POST /map/create
// enforces. Every rejection, including malformed JSON, is a *MapTemplateError.
// A missing field is reported at the object that lacks it, with an empty
// location for the top level.
func DecodeMapTemplate(data []byte) (MapTemplateView, error) {
	var document any
	if err := json.Unmarshal(data, &document); err != nil {
		return MapTemplateView{}, &MapTemplateError{Details: []*huma.ErrorDetail{{Message: "must be valid JSON: " + err.Error()}}}
	}
	registry, schema := mapFileSchema()
	result := &huma.ValidateResult{}
	huma.Validate(registry, schema, huma.NewPathBuffer(nil, 0), huma.ModeWriteToServer, document, result)
	if len(result.Errors) > 0 {
		details := make([]*huma.ErrorDetail, len(result.Errors))
		for index, err := range result.Errors {
			detail, isDetail := err.(*huma.ErrorDetail)
			if !isDetail {
				detail = &huma.ErrorDetail{Message: err.Error()}
			}
			details[index] = detail
		}
		// Huma sets the value of some errors, such as an unexpected property,
		// to the whole enclosing object. LimitErrorDetails drops those values,
		// so the file isn't copied once per problem.
		return MapTemplateView{}, &MapTemplateError{Details: httpapi.LimitErrorDetails(details)}
	}
	var template MapTemplateView
	if err := json.Unmarshal(data, &template); err != nil {
		return MapTemplateView{}, &MapTemplateError{Details: []*huma.ErrorDetail{{Message: err.Error()}}}
	}
	if err := ValidateMapTemplate(template); err != nil {
		return MapTemplateView{}, err
	}
	return template, nil
}

// MaxMapTemplateProblems is how many problems a *MapTemplateError lists. A
// final detail counts the rest, "and N more problems", so a large invalid file
// can't produce an equally large list of errors. With that detail the list
// fills httpapi.MaxErrorDetails, the bound on every problem response.
const MaxMapTemplateProblems = httpapi.MaxErrorDetails - 1

// ValidateMapTemplate checks a map file without storing it: its name and
// sizes, that player_amount is a team count a lobby can have, that every castle
// lies inside the plane, the castles' team colours, that every team colour from
// 1 to player_amount owns a castle, and the castles' upgrades. It returns nil or a *MapTemplateError listing every invalid
// field. Name uniqueness depends on storage and is checked when storing.
func ValidateMapTemplate(template MapTemplateView) error {
	var details []*huma.ErrorDetail
	reject := func(location string, value any, message string) {
		details = append(details, &huma.ErrorDetail{Location: location, Value: value, Message: message})
	}
	if strings.TrimSpace(template.MapName) == "" {
		reject("map_name", template.MapName, "must not be blank")
	}
	if template.PlayerAmount < domain.MinTeams || template.PlayerAmount > domain.MaxTeams {
		reject("player_amount", template.PlayerAmount, fmt.Sprintf("must be from %d to %d, the number of teams a lobby can have", domain.MinTeams, domain.MaxTeams))
	}
	if template.PlaneSizeX < 1 {
		reject("plane_size_x", template.PlaneSizeX, "must be at least 1")
	}
	if template.PlaneSizeY < 1 {
		reject("plane_size_y", template.PlaneSizeY, "must be at least 1")
	}
	if len(template.Castles) == 0 {
		reject("castles", template.Castles, "must contain at least one castle")
	}
	halfWidth := float64(template.PlaneSizeX) / 2
	halfHeight := float64(template.PlaneSizeY) / 2
	ownedColours := map[uint]bool{}
	for index, castle := range template.Castles {
		ownedColours[castle.TeamColor] = true
		location := fmt.Sprintf("castles[%d].", index)
		if castle.TeamColor > template.PlayerAmount {
			reject(location+"team_color", castle.TeamColor, fmt.Sprintf("must be %d for a neutral castle or a team colour from 1 to player_amount (%d)", NeutralTeamColor, template.PlayerAmount))
		}
		// Written as "inside" so that NaN, which compares false, is rejected too.
		if insidePlane := math.Abs(castle.X) <= halfWidth; !insidePlane {
			reject(location+"x", castle.X, fmt.Sprintf("must be inside the plane, from %g to %g", -halfWidth, halfWidth))
		}
		if insidePlane := math.Abs(castle.Y) <= halfHeight; !insidePlane {
			reject(location+"y", castle.Y, fmt.Sprintf("must be inside the plane, from %g to %g", -halfHeight, halfHeight))
		}
		for _, upgradeType := range slices.Sorted(maps.Keys(castle.Upgrades)) {
			if _, err := castle.Upgrades.Level(upgradeType); err != nil {
				reject(location+"upgrades."+string(upgradeType), castle.Upgrades[upgradeType], err.Error())
			}
		}
	}
	// Every lobby team is a team colour, so a colour without a castle would
	// leave a team without one. Checked only for a player_amount a lobby can
	// have, so a huge value can't produce a problem per colour.
	if template.PlayerAmount <= domain.MaxTeams {
		for colour := uint(1); colour <= template.PlayerAmount; colour++ {
			if !ownedColours[colour] {
				reject("castles", colour, fmt.Sprintf("team colour %d of player_amount %d owns no castle; every team colour from 1 to player_amount needs at least one", colour, template.PlayerAmount))
			}
		}
	}
	if len(details) > 0 {
		return &MapTemplateError{Details: httpapi.LimitErrorDetails(details)}
	}
	return nil
}

// BuildMap creates the game map that a valid template describes. Castle IDs
// follow the castles' order, and each castle starts with its own soldier count
// or the template's neutral_starting_soldier_amount.
func BuildMap(template MapTemplateView) Map {
	gameMap := Map{MapFields: template.MapFields}
	for index, castle := range template.Castles {
		soldiers := template.NeutralStartingSoldierAmount
		if castle.StartingSoldierAmount != nil {
			soldiers = *castle.StartingSoldierAmount
		}
		gameMap.Castles = append(gameMap.Castles, Castle{CastleFields: castle.fields(index), SoldierAmount: soldiers})
	}
	return gameMap
}

// NewMapTemplate converts a map file into the rows that store it.
func NewMapTemplate(template MapTemplateView) MapTemplate {
	stored := MapTemplate{MapFields: template.MapFields}
	for index, castle := range template.Castles {
		stored.CastleTemplates = append(stored.CastleTemplates, CastleTemplate{CastleFields: castle.fields(index), StartingSoldierAmount: castle.StartingSoldierAmount})
	}
	return stored
}

// View converts a stored template back into the map file format, keeping its
// castles in castle ID order.
func (template MapTemplate) View() MapTemplateView {
	castles := slices.Clone(template.CastleTemplates)
	slices.SortStableFunc(castles, func(left, right CastleTemplate) int {
		return cmp.Or(cmp.Compare(left.PersistentCastleID, right.PersistentCastleID), cmp.Compare(left.ID, right.ID))
	})
	view := MapTemplateView{MapFields: template.MapFields, Castles: make([]CastleTemplateView, len(castles))}
	for index, castle := range castles {
		view.Castles[index] = CastleTemplateView{
			TeamColor:             castle.TeamColor,
			X:                     castle.Position.P.X,
			Y:                     castle.Position.P.Y,
			StartingSoldierAmount: castle.StartingSoldierAmount,
			Upgrades:              castle.Upgrades.Clone(),
		}
	}
	return view
}

func (castle CastleTemplateView) fields(index int) CastleFields {
	return CastleFields{
		TeamColor:          castle.TeamColor,
		Position:           pgtype.Point{P: pgtype.Vec2{X: castle.X, Y: castle.Y}, Valid: true},
		PersistentCastleID: uint(index),
		Upgrades:           castle.Upgrades.Clone(),
	}
}

// MapTemplateSummaryView describes a default map in the list that the lobby's
// create form offers, published as MapTemplateSummary.
type MapTemplateSummaryView struct {
	MapTemplateID uint   `json:"map_template_id" doc:"Choose the map by passing this ID as game_settings.map_template_id when creating a lobby."`
	MapName       string `json:"map_name"`
	TeamCount     int    `json:"team_count" doc:"Teams the map is made for, its player_amount. A lobby on this map has exactly this many teams, in team colour order."`
	PlaneSizeX    uint   `json:"plane_size_x"`
	PlaneSizeY    uint   `json:"plane_size_y"`
	CastleCount   int    `json:"castle_count" doc:"Castles on the map, team and neutral."`
}

// Summary describes a stored template for the default map list. ok is false
// when the template's team colours are not exactly 1 to player_amount, so the
// map fits no lobby.
func (template MapTemplate) Summary() (summary MapTemplateSummaryView, ok bool) {
	teams, ok := MapTeamCount(template.View())
	return MapTemplateSummaryView{
		MapTemplateID: template.ID,
		MapName:       template.MapName,
		TeamCount:     teams,
		PlaneSizeX:    template.PlaneSizeX,
		PlaneSizeY:    template.PlaneSizeY,
		CastleCount:   len(template.CastleTemplates),
	}, ok
}
