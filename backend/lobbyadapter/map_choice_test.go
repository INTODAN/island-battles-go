package lobbyadapter_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/INTODAN/mpl/gamehooks"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/INTODAN/mpl/domain"
	"islandBattles/backend/dal"
	"islandBattles/backend/internal/testutils"
	"islandBattles/backend/lobbyadapter"
	"islandBattles/backend/models"
	"islandBattles/backend/services"

	gormpostgres "gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// A two-team map with a neutral castle between the teams.
const twoTeamMapFile = `{"map_name":"Strait","player_amount":2,"plane_size_x":10,"plane_size_y":6,"neutral_starting_soldier_amount":5,` +
	`"castles":[{"team_color":1,"x":-4,"y":0,"starting_soldier_amount":40,"upgrades":{"speed":2}},{"team_color":0,"x":0,"y":0},{"team_color":2,"x":4,"y":1,"starting_soldier_amount":40}]}`

// noMap refuses settings that choose no map.
const noMap = "game_settings: must choose a map: map_template_id for a default map from GET /map/templates, or map for a map file"

func storeTemplate(t *testing.T, database *gorm.DB, mapFile string) uint {
	t.Helper()
	template, err := models.DecodeMapTemplate([]byte(mapFile))
	if err != nil {
		t.Fatal(err)
	}
	templateID, err := dal.CreateMapTemplate(database, template)
	if err != nil {
		t.Fatal(err)
	}
	return templateID
}

func TestRegistrationValidatesTheMapChoice(t *testing.T) {
	database := testutils.InitTestDB(t)
	templateID := storeTemplate(t, database, twoTeamMapFile)
	policy := hookedPolicy(t, lobbyadapter.Registration(database, &testutils.RecordedPublisher{}, nil, services.ServerBots{}, nil))
	twoTeams := []domain.TeamDefinition{{Name: "Blue"}, {Name: "Red"}}
	threeTeams := slices.Concat(twoTeams, []domain.TeamDefinition{{Name: "Green"}})
	ctx := context.Background()
	for _, settings := range []string{fmt.Sprintf(`{"map_template_id":%d}`, templateID), `{"map":` + twoTeamMapFile + `}`, `{"map":` + twoTeamMapFile + `,"turn_timeout_seconds":null}`, fmt.Sprintf(`{"map_template_id":%d,"turn_timeout_seconds":600}`, templateID), fmt.Sprintf(`{"map_template_id":%d,"max_generations":50}`, templateID), fmt.Sprintf(`{"map_template_id":%d,"max_generations":1200}`, templateID)} {
		if _, err := policy.ValidateGameSettings(ctx, lobbyadapter.GameKey, "", json.RawMessage(settings), twoTeams); err != nil {
			t.Errorf("settings %s rejected: %v", settings, err)
		}
	}
	rejections := []struct {
		settings string
		teams    []domain.TeamDefinition
		message  string
	}{
		{fmt.Sprintf(`{"map_template_id":%d}`, templateID+1), twoTeams, fmt.Sprintf("game_settings.map_template_id: no default map has ID %d", templateID+1)},
		{fmt.Sprintf(`{"map_template_id":%d}`, templateID), threeTeams, "game_settings.map_template_id: map \"Strait\" has player_amount 2"},
		{`{"map":` + twoTeamMapFile + `}`, threeTeams, "game_settings.map: map \"Strait\" has player_amount 2"},
		{`{"map":` + strings.Replace(twoTeamMapFile, `"x":4`, `"x":6`, 1) + `}`, twoTeams, "game_settings.map.castles[2].x: must be inside the plane, from -5 to 5"},
		{fmt.Sprintf(`{"map_template_id":%d,"map":%s}`, templateID, twoTeamMapFile), twoTeams, "game_settings.map: cannot be combined with map_template_id"},
		{`{"map":"Strait"}`, twoTeams, "game_settings.map: expected object"},
		{fmt.Sprintf(`{"map_template_id":%d,"turn_timeout_seconds":3}`, templateID), twoTeams, "game_settings.turn_timeout_seconds: must be a whole number of seconds from 5 to 600, or null for no limit"},
		{fmt.Sprintf(`{"map_template_id":%d,"max_generations":49}`, templateID), twoTeams, "game_settings.max_generations: must be a whole number of turns from 50 to 1200"},
		{fmt.Sprintf(`{"map_template_id":%d,"max_generations":1201}`, templateID), twoTeams, "game_settings.max_generations: must be a whole number of turns from 50 to 1200"},
		{fmt.Sprintf(`{"map_template_id":%d,"max_generations":50.5}`, templateID), twoTeams, "game_settings.max_generations: must be a whole number of turns from 50 to 1200"},
		{fmt.Sprintf(`{"map_template_id":%d,"max_generations":"300"}`, templateID), twoTeams, "game_settings.max_generations: must be a whole number of turns from 50 to 1200"},
		// Every game starts on a default map or a map file.
		{``, twoTeams, noMap},
		{`{}`, twoTeams, noMap},
		{`{"turn_timeout_seconds":null}`, twoTeams, noMap},
	}
	for _, rejection := range rejections {
		_, err := policy.ValidateGameSettings(ctx, lobbyadapter.GameKey, "", json.RawMessage(rejection.settings), rejection.teams)
		if domain.ErrorCodeOf(err) != domain.CodeInvalidGameSettings || !strings.HasPrefix(err.Error(), rejection.message) {
			t.Errorf("settings %.60s with %d teams: error = %v, want prefix %q", rejection.settings, len(rejection.teams), err, rejection.message)
		}
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := policy.ValidateGameSettings(canceled, lobbyadapter.GameKey, "", json.RawMessage(fmt.Sprintf(`{"map_template_id":%d}`, templateID)), twoTeams); !errors.Is(err, context.Canceled) {
		t.Fatalf("a check that could not run = %v", err)
	}
}

func TestStartedLobbyPlaysOnTheChosenMap(t *testing.T) {
	database := testutils.InitTestDB(t)
	templateID := storeTemplate(t, database, twoTeamMapFile)
	template, err := models.DecodeMapTemplate([]byte(twoTeamMapFile))
	if err != nil {
		t.Fatal(err)
	}
	for name, settings := range map[string]string{
		"default map": fmt.Sprintf(`{"map_template_id":%d}`, templateID),
		"map file":    `{"map":` + twoTeamMapFile + `}`,
	} {
		t.Run(name, func(t *testing.T) {
			snapshot := startedSnapshot()
			snapshot.Lobby.GameSettings = json.RawMessage(settings)
			gameID, err := start(t, hooksClient(t, database, &testutils.RecordedPublisher{}), snapshot)
			if err != nil {
				t.Fatal(err)
			}
			game, err := dal.GetGameByID(database, gameID)
			if err != nil {
				t.Fatal(err)
			}
			started := game.Generations[0].Map
			expected := models.BuildMap(template)
			if started.MapFields != expected.MapFields || len(started.Castles) != len(expected.Castles) {
				t.Fatalf("started map = %+v, want %+v", started.MapFields, expected.MapFields)
			}
			for index, castle := range started.Castles {
				want := expected.Castles[index]
				if castle.PersistentCastleID != want.PersistentCastleID || castle.TeamColor != want.TeamColor || castle.SoldierAmount != want.SoldierAmount ||
					castle.Position.P != want.Position.P || !reflect.DeepEqual(castle.Upgrades, want.Upgrades) {
					t.Fatalf("castle %d = %+v, want %+v", index, castle, want)
				}
			}
		})
	}
}

// A lobby whose settings choose no map, as settings could before every game
// needed one, doesn't start. Its round gets no game, so the lobby can be
// reopened and given a map.
func TestStartWithoutAMapChoiceIsRefused(t *testing.T) {
	database := testutils.InitTestDB(t)
	client := hooksClient(t, database, &testutils.RecordedPublisher{})
	for _, settings := range []string{``, domain.EmptyGameSettings, `{"turn_timeout_seconds":30}`} {
		snapshot := startedSnapshot()
		snapshot.Lobby.GameSettings = json.RawMessage(settings)
		if gameID, err := start(t, client, snapshot); err == nil {
			t.Fatalf("settings %q started game %d", settings, gameID)
		}
		ended, err := dal.LobbyRoundEnded(database, snapshot.Lobby.ID, snapshot.Lobby.Round)
		if err != nil || !ended {
			t.Fatalf("settings %q: round ended = %v, %v; want a round without a game", settings, ended, err)
		}
	}
	var games int64
	if err := database.Model(&models.Game{}).Count(&games).Error; err != nil || games != 0 {
		t.Fatalf("games = %d, %v", games, err)
	}
}

// A lookup that can't reach the database is not the host's mistake, so the
// lobby reports it as unavailable rather than as invalid settings.
func TestRegistrationReportsAnUnreachableDatabaseAsUnavailable(t *testing.T) {
	// Port 1 refuses connections; the ping is skipped so opening succeeds.
	unreachable, err := gorm.Open(gormpostgres.Open("host=127.0.0.1 port=1 user=nobody dbname=nothing sslmode=disable connect_timeout=2"), &gorm.Config{DisableAutomaticPing: true})
	if err != nil {
		t.Fatal(err)
	}
	policy := hookedPolicy(t, lobbyadapter.Registration(unreachable, &testutils.RecordedPublisher{}, nil, services.ServerBots{}, nil))
	teams := []domain.TeamDefinition{{Name: "Blue"}, {Name: "Red"}}
	_, err = policy.ValidateGameSettings(context.Background(), lobbyadapter.GameKey, "", json.RawMessage(`{"map_template_id":1}`), teams)
	if !errors.Is(err, domain.ErrUnavailable) || domain.ErrorCodeOf(err) == domain.CodeInvalidGameSettings {
		t.Fatalf("unreachable database = %v", err)
	}
	// Choices that need no lookup are still checked without the database.
	if _, err := policy.ValidateGameSettings(context.Background(), lobbyadapter.GameKey, "", json.RawMessage(`{"map":`+twoTeamMapFile+`}`), teams); err != nil {
		t.Fatalf("inline map = %v", err)
	}
}

// hookedPolicy is a lobby policy that calls registration's hooks over HTTP,
// signed, as the lobby calls Island Battles.
func hookedPolicy(t *testing.T, registration domain.GameRegistration) domain.Policy {
	t.Helper()
	client, err := gamehooks.NewClient(lobbyadapter.GameKey, hooksServer(t, registration), hookSecret)
	if err != nil {
		t.Fatal(err)
	}
	return domain.DefaultPolicy(client.Registration(configuredGames(t)[0].Registration.StartRules, domain.LobbyRetention{}))
}
