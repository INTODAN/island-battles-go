package services_test

import (
	"context"
	"errors"
	"testing"

	"github.com/INTODAN/mpl/domain"
	"islandBattles/backend/models"
	"islandBattles/backend/services"

	"gorm.io/gorm"
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

// The owner hides Alex's account, and Alex deletes it and comes back: the
// account stays hidden, with a new hand play, and after a rated game of the
// bot name Alex registers then, the leaderboard lists only Sam's bot.
func TestAHiddenAccountStaysHiddenAfterItDeletesItself(t *testing.T) {
	ctx := context.Background()
	var alex domain.Principal
	fixture := newRatedFixtureWith(t, capSettings(1, 1, 2), true, 2, func(database *gorm.DB, person domain.Principal) {
		if person.DisplayName != ratedPeople[0] {
			return
		}
		alex = person
		accounts, leaderboard := services.Accounts{Database: database}, services.Leaderboard{Database: database}
		first, err := accounts.Read(ctx, alex)
		if err != nil {
			t.Fatal(err)
		}
		if err := leaderboard.SetAccountHidden(ctx, alex.AccountID, true); err != nil {
			t.Fatal(err)
		}
		if err := accounts.Delete(ctx, alex); err != nil {
			t.Fatal(err)
		}
		// GET /account after the deletion.
		again, err := accounts.Read(ctx, alex)
		if err != nil || again.Hand.ID == first.Hand.ID || again.Hand.Rating != models.InitialRating || len(again.BotNames) != 0 {
			t.Fatalf("Alex's account after the deletion = %+v, %v; want a new hand play and no bot name", again, err)
		}
		var hand models.Competitor
		if err := database.Unscoped().Where(&models.Competitor{ID: first.Hand.ID}).First(&hand).Error; err != nil || !hand.DeletedAt.Valid {
			t.Fatalf("Alex's first hand play = %+v, %v; want it deleted", hand, err)
		}
		assertAccountHidden(t, database, alex.AccountID)
	})

	fixture.playFirstTurn(t)
	leaderboard := services.Leaderboard{Database: fixture.database}
	listed, err := leaderboard.List(ctx, "", models.DefaultLeaderboardLimit, 0)
	if err != nil || listed.Total != 1 || len(listed.Entries) != 1 || listed.Entries[0].CompetitorID != fixture.competitors[1].ID {
		t.Fatalf("leaderboard = %+v, %v; want Leviathan alone", listed, err)
	}
	// Kraken won its rated game, but isn't listed.
	if kraken := fixture.competitor(t, fixture.competitors[0].ID); kraken.Games != 1 || kraken.Rating != 1520 {
		t.Fatalf("Kraken after the game = %+v", kraken)
	}
	var rejection *services.GenerationError
	if _, err := leaderboard.Competitor(ctx, fixture.competitors[0].ID); !errors.As(err, &rejection) || rejection.Status != 404 {
		t.Fatalf("Kraken's page = %v, want 404", err)
	}
	assertAccountHidden(t, fixture.database, alex.AccountID)
}

// assertAccountHidden checks that the account's row is stored, hidden.
func assertAccountHidden(t *testing.T, database *gorm.DB, accountID string) {
	t.Helper()
	var accounts []models.IslandAccount
	if err := database.Where(&models.IslandAccount{AccountID: accountID}).Find(&accounts).Error; err != nil || len(accounts) != 1 || !accounts[0].Hidden {
		t.Fatalf("account rows = %+v, %v; want one, hidden", accounts, err)
	}
}
