package services_test

import (
	"context"
	"testing"

	"islandBattles/backend/models"
	"islandBattles/backend/services"
)

// Two new bot names are listed once their first rated game ends, with one
// game each, and the winner's page shows the game against the loser.
func TestARatedGameListsItsCompetitorsOnTheLeaderboard(t *testing.T) {
	fixture := newRatedFixture(t, capSettings(1, 1, 2), true, 2)
	leaderboard := services.Leaderboard{Database: fixture.database}
	ctx := context.Background()
	before, err := leaderboard.List(ctx, "", models.DefaultLeaderboardLimit, 0)
	if err != nil || before.Total != 0 || len(before.Entries) != 0 {
		t.Fatalf("leaderboard before the game = %+v, %v; want nobody", before, err)
	}

	fixture.playFirstTurn(t)
	after, err := leaderboard.List(ctx, "", models.DefaultLeaderboardLimit, 0)
	if err != nil || after.Total != 2 || len(after.Entries) != 2 {
		t.Fatalf("leaderboard after the game = %+v, %v; want both bots", after, err)
	}
	for index, entry := range after.Entries {
		if entry.Rank != index+1 || entry.CompetitorID != fixture.competitors[index].ID || entry.Name != ratedBots[index] || entry.Owner == nil || *entry.Owner != ratedPeople[index] ||
			entry.Rating != []int{1520, 1480}[index] || entry.Games != 1 || entry.Wins+entry.Losses != 1 || entry.LastPlayedAt.IsZero() {
			t.Fatalf("entry %d = %+v", index, entry)
		}
	}

	page, err := leaderboard.Competitor(ctx, fixture.competitors[0].ID)
	if err != nil || page.Rank != 1 || len(page.History) != 1 {
		t.Fatalf("Kraken's page = %+v, %v", page, err)
	}
	result := page.History[0]
	if result.Result != models.RatingWin || result.RatingBefore != 1500 || result.RatingAfter != 1520 || result.EndedAt.IsZero() || len(result.Opponents) != 1 {
		t.Fatalf("Kraken's result = %+v", result)
	}
	if opponent := result.Opponents[0]; opponent.CompetitorID == nil || *opponent.CompetitorID != fixture.competitors[1].ID || opponent.Name != "Leviathan" ||
		opponent.Owner == nil || *opponent.Owner != "Sam" || opponent.RatingBefore != 1500 {
		t.Fatalf("Kraken's opponent = %+v", opponent)
	}
}
