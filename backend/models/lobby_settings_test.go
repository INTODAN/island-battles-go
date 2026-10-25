package models

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"
)

func TestParseLobbySettingsReadsOneMapChoice(t *testing.T) {
	if settings, err := ParseLobbySettings(json.RawMessage(`{}`)); err != nil || !reflect.DeepEqual(settings, LobbySettings{}) {
		t.Fatalf("empty settings = %+v, %v", settings, err)
	}
	if settings, err := ParseLobbySettings(json.RawMessage(`{"map_template_id":7}`)); err != nil || settings.MapTemplateID != 7 || settings.Map != nil {
		t.Fatalf("template settings = %+v, %v", settings, err)
	}
	settings, err := ParseLobbySettings(json.RawMessage(`{"map":` + exampleMapFile + `}`))
	expected, decodeErr := DecodeMapTemplate([]byte(exampleMapFile))
	if err != nil || decodeErr != nil || settings.Map == nil || !reflect.DeepEqual(*settings.Map, expected) {
		t.Fatalf("inline map settings = %+v, %v", settings, err)
	}
}

func TestParseLobbySettingsLocatesEveryRejection(t *testing.T) {
	outsidePlane := `{"map_name":"a","player_amount":2,"plane_size_x":4,"plane_size_y":4,"neutral_starting_soldier_amount":1,"castles":[{"team_color":1,"x":3,"y":0},{"team_color":2,"x":0,"y":9}]}`
	tests := []struct {
		name, settings string
		locations      []string
	}{
		{"not an object", `[1]`, []string{"game_settings"}},
		{"unknown setting", `{"rounds":3}`, []string{"game_settings.rounds"}},
		{"both choices", `{"map_template_id":1,"map":` + exampleMapFile + `}`, []string{"game_settings.map"}},
		{"zero template", `{"map_template_id":0}`, []string{"game_settings.map_template_id"}},
		{"negative template", `{"map_template_id":-1}`, []string{"game_settings.map_template_id"}},
		{"fractional template", `{"map_template_id":1.5}`, []string{"game_settings.map_template_id"}},
		{"string template", `{"map_template_id":"1"}`, []string{"game_settings.map_template_id"}},
		{"null template", `{"map_template_id":null}`, []string{"game_settings.map_template_id"}},
		{"map not an object", `{"map":"archipelago"}`, []string{"game_settings.map"}},
		{"map missing a field", `{"map":{"map_name":"a","player_amount":2,"plane_size_x":4,"plane_size_y":4,"castles":[{"team_color":1,"x":0,"y":0}]}}`, []string{"game_settings.map"}},
		{"castles outside the plane", `{"map":` + outsidePlane + `}`, []string{"game_settings.map.castles[0].x", "game_settings.map.castles[1].y"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := ParseLobbySettings(json.RawMessage(test.settings))
			var invalidSettings *LobbySettingsError
			if !errors.As(err, &invalidSettings) {
				t.Fatalf("expected a LobbySettingsError, got %v", err)
			}
			locations := []string{}
			for _, detail := range invalidSettings.Details {
				locations = append(locations, detail.Location)
			}
			if !reflect.DeepEqual(locations, test.locations) {
				t.Fatalf("locations = %v, want %v (%v)", locations, test.locations, err)
			}
		})
	}
}

func TestLobbySettingsErrorNamesTheField(t *testing.T) {
	_, err := ParseLobbySettings(json.RawMessage(`{"map":{"map_name":"a","player_amount":2,"plane_size_x":4,"plane_size_y":4,"neutral_starting_soldier_amount":1,"castles":[{"team_color":1,"x":3,"y":0}]}}`))
	if err == nil || err.Error() != "game_settings.map.castles[0].x: must be inside the plane, from -2 to 2" {
		t.Fatalf("message = %v", err)
	}
}

func TestCheckMapTeamsNeedsExactlyTheLobbyTeams(t *testing.T) {
	mapWith := func(playerAmount uint, colours ...uint) MapTemplateView {
		template := MapTemplateView{MapFields: MapFields{MapName: "test", PlayerAmount: playerAmount}}
		for _, colour := range colours {
			template.Castles = append(template.Castles, CastleTemplateView{TeamColor: colour})
		}
		return template
	}
	tests := []struct {
		name     string
		template MapTemplateView
		teams    int
		fits     bool
	}{
		{"two teams and a neutral castle", mapWith(2, 1, 0, 2, 2), 2, true},
		{"three teams", mapWith(3, 3, 1, 2), 3, true},
		{"more teams than the map", mapWith(2, 1, 2), 3, false},
		{"fewer teams than the map", mapWith(3, 1, 2, 3), 2, false},
		{"a colour without a castle", mapWith(3, 1, 3), 3, false},
		{"player amount above the colours", mapWith(3, 1, 2), 2, false},
		{"only neutral castles", mapWith(2, 0, 0), 2, false},
		{"a colour above player amount", mapWith(2, 1, 2, 3), 2, false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := CheckMapTeams(test.template, test.teams, "map")
			if test.fits != (err == nil) {
				t.Fatalf("CheckMapTeams = %v, want fits=%v", err, test.fits)
			}
			var invalidSettings *LobbySettingsError
			if err != nil && (!errors.As(err, &invalidSettings) || invalidSettings.Details[0].Location != "game_settings.map") {
				t.Fatalf("rejection = %#v", err)
			}
		})
	}
	err := CheckMapTeams(mapWith(3, 1, 2, 0, 3), 2, "map_template_id")
	if err == nil || err.Error() != `game_settings.map_template_id: map "test" has player_amount 3 and team colours [1 2 3], but this lobby has 2 teams, so it needs player_amount 2 and team colours exactly 1 to 2 (0 is neutral)` {
		t.Fatalf("message = %v", err)
	}
}
