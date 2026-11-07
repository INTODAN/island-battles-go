package simulation

import (
	"slices"

	"islandBattles/backend/models"
)

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
	standing := standingTeams(state, teams, nil)
	switch {
	case len(standing) == 0:
		return &models.GameOutcome{Reason: models.GameOutcomeAllEliminated}
	case len(standing) == 1:
		return &models.GameOutcome{Reason: models.GameOutcomeLastTeamStanding, WinnerTeamColor: &standing[0]}
	case !reachedCap:
		return nil
	}
	return &models.GameOutcome{Reason: models.GameOutcomeGenerationCap, WinnerTeamColor: mostCastles(state, standing)}
}

// TimeoutOutcome ends a game whose turn passed its deadline on the board of
// that turn's generation. Every team in timedOut lost. Among the other teams
// that are still standing, the team with the most castles wins, as at the
// generation cap, and equal castle counts are a draw. When no other team is
// standing, every team lost, which is a draw too. The outcome's TimedOut is
// left to the caller, which knows the players.
func TimeoutOutcome(state GameState, teams []models.GameTeam, timedOut []uint) *models.GameOutcome {
	return &models.GameOutcome{Reason: models.GameOutcomeTurnTimeout, WinnerTeamColor: mostCastles(state, standingTeams(state, teams, timedOut))}
}

// standingTeams lists the colours of teams, leaving out those in excluded,
// that still have castles or soldier groups.
func standingTeams(state GameState, teams []models.GameTeam, excluded []uint) []uint {
	castles, soldierGroups := state.CastleCounts(), state.SoldierGroupCounts()
	standing := []uint{}
	for _, team := range teams {
		if !slices.Contains(excluded, team.Color) && (castles[team.Color] > 0 || soldierGroups[team.Color] > 0) {
			standing = append(standing, team.Color)
		}
	}
	return standing
}

// mostCastles returns the colour of the team in standing with the most
// castles, or nil for a draw: a tie for the most castles, or no team.
func mostCastles(state GameState, standing []uint) *uint {
	castles := state.CastleCounts()
	var winner *uint
	mostCastles := -1
	for index, color := range standing {
		switch count := castles[color]; {
		case count > mostCastles:
			mostCastles, winner = count, &standing[index]
		case count == mostCastles:
			winner = nil
		}
	}
	return winner
}
