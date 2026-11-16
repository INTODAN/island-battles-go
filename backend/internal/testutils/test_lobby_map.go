package testutils

import (
	"encoding/json"
	"math"

	"islandBattles/backend/models"
)

// CircleMap is a map file for teams teams: one castle per team, in team colour
// order, with 30 soldiers each, on a circle of radius 2 around the centre of
// an 8 by 6 plane, the first at (-2, 0). Castle IDs are 0 to teams-1. Tests
// start lobby games on it, since every game starts on a chosen map.
func CircleMap(teams int) models.MapTemplateView {
	soldiers := uint(30)
	template := models.MapTemplateView{MapFields: models.MapFields{MapName: "Circle", PlayerAmount: uint(teams), PlaneSizeX: 8, PlaneSizeY: 6}}
	for index := range teams {
		angle := math.Pi + 2*math.Pi*float64(index)/float64(teams)
		template.Castles = append(template.Castles, models.CastleTemplateView{TeamColor: uint(index + 1), X: 2 * math.Cos(angle), Y: 2 * math.Sin(angle), StartingSoldierAmount: &soldiers})
	}
	return template
}

// CircleMapSettings are a lobby's game settings for teams teams: settings, a
// JSON object or empty for none, with CircleMap(teams) as their map unless
// they choose one.
func CircleMapSettings(teams int, settings string) json.RawMessage {
	fields := map[string]any{}
	if settings != "" {
		if err := json.Unmarshal([]byte(settings), &fields); err != nil {
			panic(err)
		}
	}
	_, hasMap := fields["map"]
	_, hasTemplate := fields["map_template_id"]
	if !hasMap && !hasTemplate {
		fields["map"] = CircleMap(teams)
	}
	combined, err := json.Marshal(fields)
	if err != nil {
		panic(err)
	}
	return combined
}
