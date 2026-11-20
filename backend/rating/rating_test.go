package rating_test

import (
	"fmt"
	"math"
	"math/rand"
	"slices"
	"testing"

	"islandBattles/backend/rating"
)

// tolerance absorbs floating point rounding in sums of changes.
const tolerance = 1e-9

func winner(index int) *int { return &index }

func assertChanges(t *testing.T, got, want []float64) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("changes = %v, want %v", got, want)
	}
	for index := range got {
		if math.Abs(got[index]-want[index]) > tolerance {
			t.Fatalf("changes = %v, want %v", got, want)
		}
	}
}

func TestTheWorkedExamples(t *testing.T) {
	newcomer := rating.Team{Rating: 1500}
	// Two new competitors: the winner goes to 1520 and the loser to 1480.
	assertChanges(t, rating.Changes([]rating.Team{newcomer, newcomer}, winner(0)), []float64{20, -20})
	assertChanges(t, rating.Changes([]rating.Team{newcomer, newcomer}, winner(1)), []float64{-20, 20})
	// A draw between them changes nothing.
	assertChanges(t, rating.Changes([]rating.Team{newcomer, newcomer}, nil), []float64{0, 0})
	// Three new competitors: the winner gains 20, each other team loses 10.
	assertChanges(t, rating.Changes([]rating.Team{newcomer, newcomer, newcomer}, winner(1)), []float64{-10, 20, -10})
	assertChanges(t, rating.Changes([]rating.Team{newcomer, newcomer, newcomer}, nil), []float64{0, 0, 0})
	// A competitor's 31st rated game uses K 20: after 30 games it gains 10
	// where a new competitor, with K 40, loses 20.
	established := rating.Team{Rating: 1500, Games: 30}
	assertChanges(t, rating.Changes([]rating.Team{established, newcomer}, winner(0)), []float64{10, -20})
	assertChanges(t, rating.Changes([]rating.Team{{Rating: 1500, Games: 29}, newcomer}, winner(0)), []float64{20, -20})
	// 400 points apart, the favourite expects 10/11 of the score.
	favourite, underdog := rating.Team{Rating: 1700}, rating.Team{Rating: 1300}
	assertChanges(t, rating.Changes([]rating.Team{favourite, underdog}, winner(0)), []float64{40.0 / 11, -40.0 / 11})
	assertChanges(t, rating.Changes([]rating.Team{favourite, underdog}, winner(1)), []float64{-400.0 / 11, 400.0 / 11})
	assertChanges(t, rating.Changes([]rating.Team{favourite, underdog}, nil), []float64{-180.0 / 11, 180.0 / 11})
	// One team, or none, changes nothing.
	assertChanges(t, rating.Changes([]rating.Team{newcomer}, winner(0)), []float64{0})
	assertChanges(t, rating.Changes(nil, nil), []float64{})
}

func TestK(t *testing.T) {
	for games, want := range map[int]int{0: 40, 1: 40, 29: 40, 30: 20, 31: 20, 500: 20} {
		if got := rating.K(games); got != want {
			t.Errorf("K(%d) = %d, want %d", games, got, want)
		}
	}
}

// Random games of two to six teams, with random ratings and games played,
// keep Elo's promises: the winner never loses rating and no other team gains;
// in a draw the highest rated team never gains and the lowest never loses,
// so a draw of two moves their ratings toward each other or not at all; with
// two teams of equal K the changes add up to zero; and no change exceeds the
// competitor's K.
func TestRandomGamesKeepTheRulesPromises(t *testing.T) {
	for seed := int64(1); seed <= 3; seed++ {
		t.Run(fmt.Sprint(seed), func(t *testing.T) {
			random := rand.New(rand.NewSource(seed))
			for range 2000 {
				teams := make([]rating.Team, 2+random.Intn(5))
				for index := range teams {
					teams[index] = rating.Team{Rating: 800 + random.Float64()*1600, Games: random.Intn(60)}
				}
				var won *int
				if random.Intn(3) > 0 {
					won = winner(random.Intn(len(teams)))
				}
				checkGame(t, teams, won, rating.Changes(teams, won))
			}
		})
	}
}

func checkGame(t *testing.T, teams []rating.Team, won *int, changes []float64) {
	t.Helper()
	fail := func(rule string) {
		t.Helper()
		t.Fatalf("%s: teams %+v, winner %v, changes %v", rule, teams, describe(won), changes)
	}
	if len(changes) != len(teams) {
		fail("one change per team")
	}
	for index, change := range changes {
		if math.Abs(change) > float64(rating.K(teams[index].Games)) {
			fail("a change exceeds the competitor's K")
		}
	}
	if won != nil {
		for index, change := range changes {
			if index == *won && change < 0 || index != *won && change > 0 {
				fail("the winner lost rating, or another team gained")
			}
		}
	} else {
		highest := slices.IndexFunc(teams, func(team rating.Team) bool {
			return team.Rating == slices.MaxFunc(teams, byRating).Rating
		})
		lowest := slices.IndexFunc(teams, func(team rating.Team) bool {
			return team.Rating == slices.MinFunc(teams, byRating).Rating
		})
		if changes[highest] > tolerance || changes[lowest] < -tolerance {
			fail("a draw moved the highest rating up or the lowest down")
		}
		if len(teams) == 2 {
			before := math.Abs(teams[0].Rating - teams[1].Rating)
			after := math.Abs(teams[0].Rating + changes[0] - teams[1].Rating - changes[1])
			if after > before+tolerance {
				fail("a draw of two moved their ratings apart")
			}
		}
	}
	if len(teams) == 2 && rating.K(teams[0].Games) == rating.K(teams[1].Games) && math.Abs(changes[0]+changes[1]) > tolerance {
		fail("two teams of equal K don't add up to zero")
	}
}

func byRating(a, b rating.Team) int {
	switch {
	case a.Rating < b.Rating:
		return -1
	case a.Rating > b.Rating:
		return 1
	}
	return 0
}

func describe(won *int) string {
	if won == nil {
		return "none (a draw)"
	}
	return fmt.Sprint(*won)
}
