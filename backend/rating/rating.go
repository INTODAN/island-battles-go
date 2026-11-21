// Package rating is the Elo rule of rated games, with no database: how much
// each team's competitor gains or loses when a rated game ends.
//
// For a game of N teams, the expected score of A against B is
// E = 1 / (1 + 10^((R_B - R_A) / 400)), and A's change from the pair (A, B)
// is (K_A / (N - 1)) * (S - E), with S 1 if A won, 0 if B won and 0.5 in a
// draw. With a winner, only the pairs that include the winner count: the
// winner gets the sum over every other team, and each other team its one
// pair against the winner. Without a winner, the teams that survived, still
// in the game when it ended, draw each other, and each team that dropped out
// loses to each survivor; two teams that dropped out aren't scored against
// each other. When no team survived, every pair counts, each as a draw. Every
// change uses the ratings from before the game, and each competitor its own
// K, so a game's changes need not add up to zero.
package rating

import (
	"math"
	"slices"
)

const (
	// ProvisionalK is a competitor's K while it has played fewer than
	// ProvisionalGames rated games.
	ProvisionalK = 40
	// EstablishedK is a competitor's K from its ProvisionalGames-th rated
	// game on.
	EstablishedK = 20
	// ProvisionalGames is how many rated games a competitor plays with
	// ProvisionalK.
	ProvisionalGames = 30
)

// Team is one team of a rated game: its competitor's rating, how many rated
// games the competitor played before this one, and whether the team survived.
type Team struct {
	Rating float64
	Games  int
	// Survived reports whether the team was still in the game when it ended,
	// as opposed to eliminated, out of time, resigned or left. It counts only
	// in a game without a winner.
	Survived bool
}

// K is the K of a competitor that played games rated games before this one:
// ProvisionalK for its first ProvisionalGames, then EstablishedK.
func K(games int) int {
	if games < ProvisionalGames {
		return ProvisionalK
	}
	return EstablishedK
}

// Expected is the expected score of a competitor rated rating against one
// rated opponent: 0.5 between equal ratings, more for the higher one.
func Expected(rating, opponent float64) float64 {
	return 1 / (1 + math.Pow(10, (opponent-rating)/400))
}

// AnySurvived reports whether a team of teams survived. In a game without a
// winner where none did, every pair of teams draws.
func AnySurvived(teams []Team) bool {
	return slices.ContainsFunc(teams, func(team Team) bool { return team.Survived })
}

// Changes returns each team's rating change, in the order of teams. winner
// is the index in teams of the team that won, which must be one of them, or
// nil for a game without a winner, which the teams' Survived decides. A game
// of fewer than two teams changes nothing.
func Changes(teams []Team, winner *int) []float64 {
	changes := make([]float64, len(teams))
	if len(teams) < 2 {
		return changes
	}
	pairs := float64(len(teams) - 1)
	// change is A's change from the pair (A, B), with A's score.
	change := func(a, b int, score float64) float64 {
		return float64(K(teams[a].Games)) / pairs * (score - Expected(teams[a].Rating, teams[b].Rating))
	}
	if winner == nil {
		anySurvived := AnySurvived(teams)
		for a := range teams {
			// Two teams that dropped out aren't scored against each other.
			for b := range teams {
				switch {
				case a == b:
				case !anySurvived || teams[a].Survived && teams[b].Survived:
					changes[a] += change(a, b, 0.5)
				case teams[a].Survived:
					changes[a] += change(a, b, 1)
				case teams[b].Survived:
					changes[a] += change(a, b, 0)
				}
			}
		}
		return changes
	}
	for other := range teams {
		if other != *winner {
			changes[*winner] += change(*winner, other, 1)
			changes[other] = change(other, *winner, 0)
		}
	}
	return changes
}
