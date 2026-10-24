package services

import (
	"encoding/json"
	"reflect"
	"testing"

	"islandBattles/backend/models"
	"islandBattles/lobby/domain"
)

// The generated lobby map written as a map file. Positions are the floating
// point results of the generator's circle, so the built map matches exactly.
const twoTeamLobbyMapFile = `{
  "map_name": "Lobby battle",
  "player_amount": 2,
  "plane_size_x": 8,
  "plane_size_y": 6,
  "neutral_starting_soldier_amount": 0,
  "castles": [
    {"team_color": 1, "x": -2, "y": 2.449293598294703e-16, "starting_soldier_amount": 30},
    {"team_color": 2, "x": 2, "y": -4.898587196589406e-16, "starting_soldier_amount": 30}
  ]
}`

const threeTeamLobbyMapFile = `{
  "map_name": "Lobby battle",
  "player_amount": 4,
  "plane_size_x": 8,
  "plane_size_y": 6,
  "neutral_starting_soldier_amount": 0,
  "castles": [
    {"team_color": 1, "x": -2, "y": 2.449293598294703e-16, "starting_soldier_amount": 30},
    {"team_color": 2, "x": 0.9999999999999986, "y": -1.7320508075688783, "starting_soldier_amount": 30},
    {"team_color": 3, "x": 1.000000000000001, "y": 1.7320508075688765, "starting_soldier_amount": 30}
  ]
}`

func TestLobbyMapIsAMapTemplate(t *testing.T) {
	for _, scenario := range []struct {
		name        string
		teamPlayers []int
		mapFile     string
	}{
		{name: "two teams", teamPlayers: []int{1, 1}, mapFile: twoTeamLobbyMapFile},
		{name: "three teams", teamPlayers: []int{2, 1, 1}, mapFile: threeTeamLobbyMapFile},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			binding := models.BindingResult{GameID: 1, LobbyID: "lobby"}
			snapshot := domain.Snapshot{Lobby: domain.Lobby{ID: "lobby"}}
			for index, players := range scenario.teamPlayers {
				team := domain.Team{ID: string(rune('a' + index)), Order: index + 1}
				snapshot.Teams = append(snapshot.Teams, team)
				binding.Teams = append(binding.Teams, models.TeamBinding{TeamID: team.ID, TeamOrder: team.Order, TeamColor: uint(team.Order)})
				for player := range players {
					snapshot.StartRoster = append(snapshot.StartRoster, domain.StartRosterEntry{PlayerID: team.ID + string(rune('0'+player)), TeamID: team.ID})
				}
			}
			_, generated := prepareLobbyGame(binding, snapshot)

			var template models.MapTemplateView
			if err := json.Unmarshal([]byte(scenario.mapFile), &template); err != nil {
				t.Fatal(err)
			}
			if err := models.ValidateMapTemplate(template); err != nil {
				t.Fatal(err)
			}
			if built := models.BuildMap(template); !reflect.DeepEqual(built, generated) {
				t.Fatalf("map file builds another map:\n got %+v\nwant %+v", built, generated)
			}
		})
	}
}
