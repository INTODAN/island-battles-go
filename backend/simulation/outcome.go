package simulation

import "islandBattles/backend/models"

// Outcome applies the end rule to the board a turn produced. It returns nil
// while the game goes on.
//
// A team is out once it has no castles and no soldier groups. The game ends
// when one of teams is left, which wins, and neutral castles never need to be
// captured. When no team is left, the remaining teams were eliminated in the
// same turn, which is a draw. Otherwise a turn that reached the generation cap
// ends the game: the team with the most castles wins, and equal castle counts
// are a draw. Colours that belong to none of teams count as neutral.
func Outcome(state GameState, teams []models.GameTeam, reachedCap bool) *models.GameOutcome {
	castles, soldierGroups := state.CastleCounts(), state.SoldierGroupCounts()
	standing := []uint{}
	for _, team := range teams {
		if castles[team.Color] > 0 || soldierGroups[team.Color] > 0 {
			standing = append(standing, team.Color)
		}
	}
	switch {
	case len(standing) == 0:
		return &models.GameOutcome{Reason: models.GameOutcomeAllEliminated}
	case len(standing) == 1:
		return &models.GameOutcome{Reason: models.GameOutcomeLastTeamStanding, WinnerTeamColor: &standing[0]}
	case !reachedCap:
		return nil
	}
	outcome := &models.GameOutcome{Reason: models.GameOutcomeGenerationCap}
	mostCastles := -1
	for index, color := range standing {
		switch count := castles[color]; {
		case count > mostCastles:
			mostCastles, outcome.WinnerTeamColor = count, &standing[index]
		case count == mostCastles:
			outcome.WinnerTeamColor = nil
		}
	}
	return outcome
}
