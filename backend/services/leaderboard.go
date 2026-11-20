package services

import (
	"context"
	"errors"
	"slices"

	"islandBattles/backend/dal"
	"islandBattles/backend/models"

	"gorm.io/gorm"
)

// Leaderboard serves the public leaderboard, which anyone reads without
// signing in, and the admin's removal of offensive names from it.
//
// It lists the competitors that are live, have played at least one rated
// game, and are house bots or belong to an account that isn't hidden. A
// bot name is shown with its owner's display name, the one the account
// chose; the game stores nothing of Google's.
type Leaderboard struct {
	Database *gorm.DB
}

// List returns a page of the leaderboard, of one kind of competitor, or of
// every kind when kind is empty, ranked within that kind.
func (leaderboard Leaderboard) List(ctx context.Context, kind models.CompetitorKind, limit, offset int) (models.LeaderboardView, error) {
	database := leaderboard.Database.WithContext(ctx)
	total, err := dal.CountListedCompetitors(database, kind)
	if err != nil {
		return models.LeaderboardView{}, accountsUnavailable(ctx, "count the leaderboard", err)
	}
	competitors, err := dal.LeaderboardPage(database, kind, limit, offset)
	if err != nil {
		return models.LeaderboardView{}, accountsUnavailable(ctx, "read the leaderboard", err)
	}
	view := models.LeaderboardView{Total: total, Entries: []models.LeaderboardEntryView{}}
	if len(competitors) == 0 {
		return view, nil
	}
	// Only the page's first competitor may tie with ones before the page.
	ahead, err := dal.CountListedAbove(database, kind, models.RoundRating(competitors[0].Rating))
	if err != nil {
		return models.LeaderboardView{}, accountsUnavailable(ctx, "rank the leaderboard", err)
	}
	accounts, err := dal.AccountsByID(database, competitorAccounts(competitors))
	if err != nil {
		return models.LeaderboardView{}, accountsUnavailable(ctx, "read the accounts", err)
	}
	rank := ahead + 1
	for index, competitor := range competitors {
		// The order is by rating, so a competitor shown below the one
		// before it has every competitor before it ahead.
		if index > 0 && models.RoundRating(competitor.Rating) != models.RoundRating(competitors[index-1].Rating) {
			rank = offset + index + 1
		}
		view.Entries = append(view.Entries, leaderboardEntry(competitor, accounts, rank))
	}
	return view, nil
}

// Competitor returns a listed competitor's page: its entry, ranked among
// every kind, and its latest rated results, newest first. A competitor that
// the leaderboard doesn't list, deleted, of a hidden account, without a
// rated game or unknown, is 404.
func (leaderboard Leaderboard) Competitor(ctx context.Context, competitorID string) (models.LeaderboardCompetitorView, error) {
	database := leaderboard.Database.WithContext(ctx)
	competitor, err := dal.FindListedCompetitor(database, competitorID)
	if err != nil {
		return models.LeaderboardCompetitorView{}, accountsUnavailable(ctx, "read the competitor", err)
	}
	if competitor == nil {
		return models.LeaderboardCompetitorView{}, reject(404, "the leaderboard lists no competitor with this ID")
	}
	ahead, err := dal.CountListedAbove(database, "", models.RoundRating(competitor.Rating))
	if err != nil {
		return models.LeaderboardCompetitorView{}, accountsUnavailable(ctx, "rank the competitor", err)
	}
	events, err := dal.CompetitorHistory(database, competitor.ID, models.CompetitorHistoryLimit)
	if err != nil {
		return models.LeaderboardCompetitorView{}, accountsUnavailable(ctx, "read the competitor's history", err)
	}
	var opponentIDs []string
	for _, event := range events {
		for _, opponent := range event.Opponents {
			opponentIDs = append(opponentIDs, opponent.CompetitorID)
		}
	}
	opponents, err := dal.CompetitorsByID(database, opponentIDs)
	if err != nil {
		return models.LeaderboardCompetitorView{}, accountsUnavailable(ctx, "read the opponents", err)
	}
	accounts, err := dal.AccountsByID(database, competitorAccounts(slices.Concat(opponents, []models.Competitor{*competitor})))
	if err != nil {
		return models.LeaderboardCompetitorView{}, accountsUnavailable(ctx, "read the accounts", err)
	}
	opponentsByID := make(map[string]models.Competitor, len(opponents))
	for _, opponent := range opponents {
		opponentsByID[opponent.ID] = opponent
	}

	view := models.LeaderboardCompetitorView{LeaderboardEntryView: leaderboardEntry(*competitor, accounts, ahead+1), History: []models.CompetitorResultView{}}
	for _, event := range events {
		result := models.CompetitorResultView{EndedAt: event.EndedAt, Result: event.Result,
			RatingBefore: models.RoundRating(event.RatingBefore), RatingAfter: models.RoundRating(event.RatingAfter), Opponents: []models.CompetitorResultOpponentView{}}
		for _, opponent := range event.Opponents {
			result.Opponents = append(result.Opponents, resultOpponent(opponent, opponentsByID, accounts))
		}
		view.History = append(view.History, result)
	}
	return view, nil
}

// RemoveBotName deletes a bot name for the admin, as its owner could: its
// rating goes with it, it leaves the leaderboard and other competitors'
// history, and the name is free at once, so the owner can register another.
// An unknown or deleted competitor is 404, and an account's hand play or a
// house bot 422.
func (leaderboard Leaderboard) RemoveBotName(ctx context.Context, competitorID string) error {
	err := dal.RemoveBotName(leaderboard.Database.WithContext(ctx), competitorID)
	switch {
	case errors.Is(err, dal.ErrCompetitorNotFound):
		return reject(404, "no live competitor has this ID")
	case errors.Is(err, dal.ErrNotABotName):
		return reject(422, "only a bot name can be removed: hide the account of a person's hand play instead (PUT /admin/accounts/{account_id}/hidden), and house bots stay")
	case err != nil:
		return accountsUnavailable(ctx, "remove the bot name", err)
	}
	return nil
}

// SetAccountHidden hides an account's hand play and every bot name of it
// from the leaderboard and from other competitors' history, for an
// offensive display name, or shows them again. Nothing is deleted, and the
// account can still play. An account that the game doesn't know is 404.
func (leaderboard Leaderboard) SetAccountHidden(ctx context.Context, accountID string, hidden bool) error {
	found, err := dal.SetAccountHidden(leaderboard.Database.WithContext(ctx), accountID, hidden)
	if err != nil {
		return accountsUnavailable(ctx, "hide the account", err)
	}
	if !found {
		return reject(404, "the game knows no account with this ID")
	}
	return nil
}

// competitorAccounts are the accounts of competitors, house bots having
// none.
func competitorAccounts(competitors []models.Competitor) []string {
	var accountIDs []string
	for _, competitor := range competitors {
		if competitor.AccountID != nil {
			accountIDs = append(accountIDs, *competitor.AccountID)
		}
	}
	return accountIDs
}

// leaderboardEntry shows a listed competitor at a rank, with its owner's
// display name for a bot name.
func leaderboardEntry(competitor models.Competitor, accounts map[string]models.IslandAccount, rank int) models.LeaderboardEntryView {
	name, owner := shownName(competitor, accounts)
	entry := models.LeaderboardEntryView{Rank: rank, CompetitorID: competitor.ID, Kind: competitor.Kind, Name: name, Owner: owner,
		Rating: models.RoundRating(competitor.Rating), Games: competitor.Games, Wins: competitor.Wins, Draws: competitor.Draws, Losses: competitor.Losses}
	if competitor.LastPlayedAt != nil {
		entry.LastPlayedAt = *competitor.LastPlayedAt
	}
	return entry
}

// shownName is the name a competitor is shown by (CompetitorView) and, for
// a bot name, its owner's display name.
func shownName(competitor models.Competitor, accounts map[string]models.IslandAccount) (string, *string) {
	var accountName string
	if competitor.AccountID != nil {
		accountName = accounts[*competitor.AccountID].DisplayName
	}
	name := CompetitorView(competitor, accountName).Name
	if competitor.Kind != models.BotCompetitor {
		return name, nil
	}
	return name, &accountName
}

// resultOpponent shows an opponent in a competitor's history by the name it
// is shown by now, or as a deleted bot or player, without its ID, when it
// was deleted since or its account is hidden.
func resultOpponent(opponent models.RatingOpponent, competitors map[string]models.Competitor, accounts map[string]models.IslandAccount) models.CompetitorResultOpponentView {
	view := models.CompetitorResultOpponentView{RatingBefore: models.RoundRating(opponent.RatingBefore)}
	competitor, found := competitors[opponent.CompetitorID]
	gone := !found || competitor.DeletedAt.Valid
	if found && competitor.AccountID != nil {
		account, known := accounts[*competitor.AccountID]
		gone = gone || !known || account.Hidden
	}
	if gone {
		view.Name = models.DeletedPlayerName
		if competitor.Kind == models.BotCompetitor {
			view.Name = models.DeletedBotName
		}
		return view
	}
	view.CompetitorID = &competitor.ID
	view.Name, view.Owner = shownName(competitor, accounts)
	return view
}
