package simulation_test

import (
	"fmt"
	"reflect"
	"testing"

	"islandBattles/backend/models"
	"islandBattles/backend/simulation"
)

// board gives each castle the team colour at its position in castleTeams and
// puts one soldier group in the field for each colour in groupTeams.
func board(castleTeams []uint, groupTeams ...uint) simulation.GameState {
	castles := make([]simulation.Castle, 0, len(castleTeams))
	for index, team := range castleTeams {
		castles = append(castles, simulation.Castle{TeamColor: team, Position: point(float64(index), 0)})
	}
	state := newState(10, castles...)
	for _, team := range groupTeams {
		placeGroup(&state, team, 5, 1)
	}
	return state
}

func teams(colors ...uint) []models.GameTeam {
	gameTeams := make([]models.GameTeam, 0, len(colors))
	for _, color := range colors {
		gameTeams = append(gameTeams, models.GameTeam{Color: color})
	}
	return gameTeams
}

func winner(reason models.GameOutcomeReason, color uint) *models.GameOutcome {
	return &models.GameOutcome{Reason: reason, WinnerTeamColor: &color}
}

func TestOutcome(t *testing.T) {
	neutral := models.NeutralTeamColor
	for _, test := range []struct {
		name       string
		state      simulation.GameState
		teams      []models.GameTeam
		reachedCap bool
		want       *models.GameOutcome
	}{
		{"two teams with castles play on", board([]uint{1, 2}), teams(1, 2), false, nil},
		{"a team with only soldier groups is still standing", board([]uint{1, 1}, 2), teams(1, 2), false, nil},
		{"the last team standing wins without the neutral castles", board([]uint{1, neutral, neutral}), teams(1, 2), false, winner(models.GameOutcomeLastTeamStanding, 1)},
		{"soldier groups alone keep the last team standing", board([]uint{neutral}, 2), teams(1, 2), false, winner(models.GameOutcomeLastTeamStanding, 2)},
		{"a team eliminated before the others stays out", board([]uint{1, 3}), teams(1, 2, 3), false, nil},
		{"teams eliminated in the same turn draw", board([]uint{neutral, neutral}), teams(1, 2), false, &models.GameOutcome{Reason: models.GameOutcomeAllEliminated}},
		{"a colour without a team counts as neutral", board([]uint{1, 4}, 4), teams(1, 2), false, winner(models.GameOutcomeLastTeamStanding, 1)},
		{"the most castles win at the cap", board([]uint{1, 2, 2, neutral, neutral, neutral}), teams(1, 2), true, winner(models.GameOutcomeGenerationCap, 2)},
		{"equal castle counts draw at the cap", board([]uint{1, 2, neutral}, 1), teams(1, 2), true, &models.GameOutcome{Reason: models.GameOutcomeGenerationCap}},
		{"a later team with more castles breaks an earlier tie", board([]uint{1, 2, 3, 3}), teams(1, 2, 3), true, winner(models.GameOutcomeGenerationCap, 3)},
		{"a tie for the most castles draws even with a smaller team", board([]uint{1, 1, 2, 3, 3}), teams(1, 2, 3), true, &models.GameOutcome{Reason: models.GameOutcomeGenerationCap}},
		{"soldier groups do not count as castles at the cap", board([]uint{1, neutral}, 2, 2), teams(1, 2), true, winner(models.GameOutcomeGenerationCap, 1)},
		{"the last team standing at the cap wins as the last team", board([]uint{1}), teams(1, 2), true, winner(models.GameOutcomeLastTeamStanding, 1)},
	} {
		t.Run(test.name, func(t *testing.T) {
			got := simulation.Outcome(test.state, test.teams, test.reachedCap)
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("outcome = %s, want %s", describe(got), describe(test.want))
			}
		})
	}
}

func TestTimeoutOutcome(t *testing.T) {
	neutral := models.NeutralTeamColor
	draw := &models.GameOutcome{Reason: models.GameOutcomeTurnTimeout}
	for _, test := range []struct {
		name     string
		state    simulation.GameState
		teams    []models.GameTeam
		timedOut []uint
		want     *models.GameOutcome
	}{
		{"the other team of two wins", board([]uint{1, 1, 1, 2}), teams(1, 2), []uint{1}, winner(models.GameOutcomeTurnTimeout, 2)},
		{"the other team wins with soldier groups alone", board([]uint{1, neutral}, 2), teams(1, 2), []uint{1}, winner(models.GameOutcomeTurnTimeout, 2)},
		{"the most castles win among the rest", board([]uint{1, 1, 1, 2, 3, 3}), teams(1, 2, 3), []uint{1}, winner(models.GameOutcomeTurnTimeout, 3)},
		{"equal castle counts among the rest draw", board([]uint{1, 1, 1, 2, 3}), teams(1, 2, 3), []uint{1}, draw},
		{"two teams timing out together both lose", board([]uint{1, 1, 2, 2, 3}), teams(1, 2, 3), []uint{1, 2}, winner(models.GameOutcomeTurnTimeout, 3)},
		{"every team timing out is a draw", board([]uint{1, 2}), teams(1, 2), []uint{1, 2}, draw},
		{"an eliminated team never wins", board([]uint{1, neutral}), teams(1, 2, 3), []uint{1}, draw},
		{"a team that is still standing beats an eliminated one", board([]uint{1, neutral}, 3), teams(1, 2, 3), []uint{1}, winner(models.GameOutcomeTurnTimeout, 3)},
	} {
		t.Run(test.name, func(t *testing.T) {
			got := simulation.TimeoutOutcome(test.state, test.teams, test.timedOut)
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("outcome = %s, want %s", describe(got), describe(test.want))
			}
		})
	}
}

func describe(outcome *models.GameOutcome) string {
	switch {
	case outcome == nil:
		return "game goes on"
	case outcome.WinnerTeamColor == nil:
		return string(outcome.Reason) + " draw"
	}
	return fmt.Sprintf("%s won by team %d", outcome.Reason, *outcome.WinnerTeamColor)
}
