package services

import (
	"cmp"
	"fmt"
	"slices"
	"time"

	"islandBattles/backend/dal"
	"islandBattles/backend/models"
	"islandBattles/backend/rating"

	"gorm.io/gorm"
)

// rateGame applies a rated game's outcome to its competitors' ratings, in
// the transaction that stores the outcome, and records the changes in
// outcome.Ratings: every caller of dal.SaveGameOutcome calls it just before.
// A game that isn't rated changes nothing. Otherwise it locks the players'
// competitors in the order of their IDs, soft-deleted ones too, so two games
// that end at once can't deadlock; works out each team's change with the Elo
// rule (backend/rating) from the ratings before the game, in which a game
// without a winner is a draw between the outcome's survivors and a loss to
// each of them for every other team; stores each
// competitor's new rating, its rated games and its win, draw or loss; and
// adds a history row for each, whose unique pair of game and competitor
// refuses to rate a game twice. Any error fails the transaction, so the
// outcome and the ratings are stored together or not at all.
func rateGame(transaction *gorm.DB, game *models.Game, players []models.FrozenGamePlayer, outcome *models.GameOutcome) error {
	if !game.Rated {
		return nil
	}
	// A team has one player, so the players in colour order are the teams.
	teams := slices.SortedFunc(slices.Values(players), func(a, b models.FrozenGamePlayer) int { return cmp.Compare(a.TeamColor, b.TeamColor) })
	competitorIDs := make([]string, 0, len(teams))
	var winner *int
	for index, player := range teams {
		if player.CompetitorID == nil {
			return fmt.Errorf("rate game %d: %s plays without a competitor", game.ID, player.DisplayName)
		}
		competitorIDs = append(competitorIDs, *player.CompetitorID)
		if outcome.WinnerTeamColor != nil && *outcome.WinnerTeamColor == player.TeamColor {
			winner = &index
		}
	}
	if outcome.WinnerTeamColor != nil && winner == nil {
		return fmt.Errorf("rate game %d: no player of the winning team %d", game.ID, *outcome.WinnerTeamColor)
	}
	locked, err := dal.LockCompetitors(transaction, competitorIDs)
	if err != nil {
		return fmt.Errorf("rate game %d: lock the competitors: %w", game.ID, err)
	}
	competitors := make(map[string]models.Competitor, len(locked))
	var accountIDs []string
	for _, competitor := range locked {
		competitors[competitor.ID] = competitor
		if competitor.AccountID != nil {
			accountIDs = append(accountIDs, *competitor.AccountID)
		}
	}
	// A rated game holds each competitor once.
	if len(competitors) != len(teams) {
		return fmt.Errorf("rate game %d: found %d competitors for %d teams", game.ID, len(competitors), len(teams))
	}
	accountNames, err := dal.AccountDisplayNames(transaction, accountIDs)
	if err != nil {
		return fmt.Errorf("rate game %d: read the accounts' names: %w", game.ID, err)
	}

	before := make([]rating.Team, len(teams))
	for index, competitorID := range competitorIDs {
		before[index] = rating.Team{Rating: competitors[competitorID].Rating, Games: competitors[competitorID].Games,
			Survived: slices.Contains(outcome.Survivors, teams[index].TeamColor)}
	}
	changes := rating.Changes(before, winner)
	endedAt := time.Now()
	events := make([]models.RatingEvent, 0, len(teams))
	updated := make([]models.Competitor, 0, len(teams))
	ratings := make([]models.RatingChange, 0, len(teams))
	for index, player := range teams {
		competitor := competitors[competitorIDs[index]]
		result := ratingResult(before, index, winner)
		event := models.RatingEvent{GameID: game.ID, CompetitorID: competitor.ID, Result: result, RatingBefore: competitor.Rating,
			RatingAfter: competitor.Rating + changes[index], K: rating.K(competitor.Games), Opponents: []models.RatingOpponent{}, EndedAt: endedAt}
		for other, opponentID := range competitorIDs {
			if other != index {
				event.Opponents = append(event.Opponents, models.RatingOpponent{CompetitorID: opponentID, RatingBefore: before[other].Rating})
			}
		}
		events = append(events, event)
		ratings = append(ratings, ratingChange(player, competitor, accountNames, event))

		competitor.Rating, competitor.LastPlayedAt = event.RatingAfter, &endedAt
		competitor.Games++
		switch result {
		case models.RatingWin:
			competitor.Wins++
		case models.RatingDraw:
			competitor.Draws++
		default:
			competitor.Losses++
		}
		updated = append(updated, competitor)
	}
	// The history goes first: a game rated before fails here, before any
	// rating changes.
	if err := dal.StoreRatingEvents(transaction, events); err != nil {
		return fmt.Errorf("rate game %d: store the rating history: %w", game.ID, err)
	}
	for _, competitor := range updated {
		if err := dal.SaveCompetitorRecord(transaction, competitor); err != nil {
			return fmt.Errorf("rate game %d: store competitor %s: %w", game.ID, competitor.ID, err)
		}
	}
	outcome.Ratings = ratings
	return nil
}

// ratingResult is the result of the team at index of teams, with the index
// of the winning team, or nil for a game without a winner: then a survivor
// drew and a team that dropped out lost, unless no team survived, which is a
// draw for every team.
func ratingResult(teams []rating.Team, index int, winner *int) models.RatingResult {
	switch {
	case winner == nil && (teams[index].Survived || !rating.AnySurvived(teams)):
		return models.RatingDraw
	case winner != nil && *winner == index:
		return models.RatingWin
	}
	return models.RatingLoss
}

// ratingChange is a team's rating change as the outcome shows it, with the
// name its competitor is shown by: a bot's name, the account's display name
// for its hand play, and Bot (level N) for a house bot; a bot also names its
// account as its owner. A bot name deleted during the game, which has lost
// its name, keeps the one the game started with, and an account the game no
// longer knows the player's display name at the start.
func ratingChange(player models.FrozenGamePlayer, competitor models.Competitor, accountNames map[string]string, event models.RatingEvent) models.RatingChange {
	accountName := player.DisplayName
	if competitor.AccountID != nil {
		if name, found := accountNames[*competitor.AccountID]; found {
			accountName = name
		}
	}
	change := models.RatingChange{TeamColor: player.TeamColor, CompetitorID: competitor.ID, Kind: competitor.Kind,
		Name: CompetitorView(competitor, accountName).Name, Before: event.RatingBefore, After: event.RatingAfter}
	if competitor.Kind == models.BotCompetitor {
		change.Owner = accountName
		if change.Name == "" && player.BotName != nil {
			change.Name = *player.BotName
		}
	}
	return change
}
