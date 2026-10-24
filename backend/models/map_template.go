package models

import (
	"cmp"
	"fmt"
	"maps"
	"math"
	"slices"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/jackc/pgx/v5/pgtype"
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

// MapTemplateError rejects a map template and lists every invalid field.
type MapTemplateError struct {
	// Details locate each problem relative to the template, such as castles[2].x.
	Details []*huma.ErrorDetail
}

func (err *MapTemplateError) Error() string {
	problems := make([]string, len(err.Details))
	for index, detail := range err.Details {
		problems[index] = detail.Location + " " + detail.Message
	}
	return "invalid map template: " + strings.Join(problems, "; ")
}

// Located places the details under location, such as "body" for a request
// body, ready to pass to a Huma error response.
func (err *MapTemplateError) Located(location string) []error {
	located := make([]error, len(err.Details))
	for index, detail := range err.Details {
		located[index] = &huma.ErrorDetail{Message: detail.Message, Location: location + "." + detail.Location, Value: detail.Value}
	}
	return located
}

// ValidateMapTemplate checks a map file without storing it: its name and
// sizes, that every castle lies inside the plane, the castles' team colours and
// their upgrades. It returns nil or a *MapTemplateError listing every invalid
// field. Name uniqueness depends on storage and is checked when storing.
func ValidateMapTemplate(template MapTemplateView) error {
	var details []*huma.ErrorDetail
	reject := func(location string, value any, message string) {
		details = append(details, &huma.ErrorDetail{Location: location, Value: value, Message: message})
	}
	if strings.TrimSpace(template.MapName) == "" {
		reject("map_name", template.MapName, "must not be blank")
	}
	if template.PlayerAmount < 1 {
		reject("player_amount", template.PlayerAmount, "must be at least 1")
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
	for index, castle := range template.Castles {
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
	if len(details) > 0 {
		return &MapTemplateError{Details: details}
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
