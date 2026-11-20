package services

import (
	"context"
	"errors"
	"fmt"

	"github.com/INTODAN/mpl/domain"
	"islandBattles/backend/dal"
	"islandBattles/backend/models"

	"gorm.io/gorm"
)

// SignInRequiredProblemType is the problem type of the 403 that the account
// routes answer a guest's player token, which names no account.
const SignInRequiredProblemType = "urn:island-battles:problem:sign-in-required"

// BotNameTakenProblemType is the problem type of the 409 that refuses a bot
// name that any account has, without regard to case.
const BotNameTakenProblemType = "urn:island-battles:problem:bot-name-taken"

// BotNameLimitProblemType is the problem type of the 409 that refuses a bot
// name beyond models.MaxBotNames.
const BotNameLimitProblemType = "urn:island-battles:problem:bot-name-limit"

// Accounts keeps the lobby's accounts as the game knows them, and their
// competitors: what later gets a rating. A player token of a signed-in
// player names their account and display name; a guest's names none, and
// has no account here.
type Accounts struct {
	Database *gorm.DB
}

// Seen stores an account the first time the game sees it, with its manual
// competitor, and its display name whenever that changed: a rename in the
// lobby reaches the game with the account's next player token.
func (accounts Accounts) Seen(ctx context.Context, accountID, displayName string) error {
	return accounts.Database.WithContext(ctx).Transaction(func(transaction *gorm.DB) error {
		return dal.SeeAccount(transaction, accountID, displayName)
	})
}

// Read returns the caller's account, with its hand play and its bot names.
func (accounts Accounts) Read(ctx context.Context, principal domain.Principal) (models.IslandAccountView, error) {
	if err := signedIn(principal); err != nil {
		return models.IslandAccountView{}, err
	}
	if err := accounts.Seen(ctx, principal.AccountID, principal.DisplayName); err != nil {
		return models.IslandAccountView{}, accountsUnavailable(ctx, "store the account", err)
	}
	competitors, err := dal.AccountCompetitors(accounts.Database.WithContext(ctx), principal.AccountID)
	if err != nil {
		return models.IslandAccountView{}, accountsUnavailable(ctx, "read the account", err)
	}
	view := models.IslandAccountView{AccountID: principal.AccountID, DisplayName: principal.DisplayName, MaxBotNames: models.MaxBotNames, BotNames: []models.CompetitorView{}}
	for _, competitor := range competitors {
		switch competitor.Kind {
		case models.ManualCompetitor:
			view.Hand = CompetitorView(competitor, principal.DisplayName)
		case models.BotCompetitor:
			view.BotNames = append(view.BotNames, CompetitorView(competitor, principal.DisplayName))
		}
	}
	return view, nil
}

// RegisterBotName registers a bot name for the caller's account, at
// models.InitialRating.
func (accounts Accounts) RegisterBotName(ctx context.Context, principal domain.Principal, name string) (models.CompetitorView, error) {
	if err := signedIn(principal); err != nil {
		return models.CompetitorView{}, err
	}
	name, err := models.NormalizeBotName(name)
	if err != nil {
		return models.CompetitorView{}, reject(422, err.Error())
	}
	var competitor models.Competitor
	err = accounts.Database.WithContext(ctx).Transaction(func(transaction *gorm.DB) error {
		// SeeAccount locks the account, so two names registered at once
		// count each other.
		if err := dal.SeeAccount(transaction, principal.AccountID, principal.DisplayName); err != nil {
			return err
		}
		var err error
		competitor, err = dal.AddBotName(transaction, principal.AccountID, name)
		return err
	})
	switch {
	case errors.Is(err, dal.ErrBotNameTaken):
		return models.CompetitorView{}, &GenerationError{Status: 409, Type: BotNameTakenProblemType,
			Detail: fmt.Sprintf("the bot name %q is taken: names are unique across all accounts, without regard to case", name)}
	case errors.Is(err, dal.ErrBotNameLimit):
		return models.CompetitorView{}, &GenerationError{Status: 409, Type: BotNameLimitProblemType,
			Detail: fmt.Sprintf("an account has at most %d bot names: delete one to register another", models.MaxBotNames)}
	case err != nil:
		return models.CompetitorView{}, accountsUnavailable(ctx, "register the bot name", err)
	}
	return CompetitorView(competitor, principal.DisplayName), nil
}

// DeleteBotName deletes one of the caller's bot names, with its rating. The
// name is free at once. Another account's bot name, or an unknown one, is
// 404.
func (accounts Accounts) DeleteBotName(ctx context.Context, principal domain.Principal, competitorID string) error {
	if err := signedIn(principal); err != nil {
		return err
	}
	if err := accounts.Seen(ctx, principal.AccountID, principal.DisplayName); err != nil {
		return accountsUnavailable(ctx, "store the account", err)
	}
	deleted, err := dal.DeleteBotName(accounts.Database.WithContext(ctx), principal.AccountID, competitorID)
	if err != nil {
		return accountsUnavailable(ctx, "delete the bot name", err)
	}
	if !deleted {
		return reject(404, "your account has no bot name with this ID")
	}
	return nil
}

// Delete deletes the caller's account: every competitor of it, with its
// rating, and the account's row. The client calls it before it deletes the
// account in the lobby, so the game's part is gone first.
func (accounts Accounts) Delete(ctx context.Context, principal domain.Principal) error {
	if err := signedIn(principal); err != nil {
		return err
	}
	if err := dal.DeleteAccount(accounts.Database.WithContext(ctx), principal.AccountID); err != nil {
		return accountsUnavailable(ctx, "delete the account", err)
	}
	return nil
}

// CompetitorView shows a competitor by the name it is shown under: a bot's
// name, its owner's display name, ownerName, for the hand play, and
// BotSeatName for a house bot.
func CompetitorView(competitor models.Competitor, ownerName string) models.CompetitorView {
	view := models.CompetitorView{ID: competitor.ID, Kind: competitor.Kind, Rating: competitor.Rating,
		Games: competitor.Games, Wins: competitor.Wins, Draws: competitor.Draws, Losses: competitor.Losses, LastPlayedAt: competitor.LastPlayedAt}
	switch {
	case competitor.Kind == models.ManualCompetitor:
		view.Name = ownerName
	case competitor.Kind == models.HouseCompetitor && competitor.HouseLevel != nil:
		view.Name = BotSeatName(*competitor.HouseLevel)
	case competitor.Name != nil:
		view.Name = *competitor.Name
	}
	return view
}

// signedIn refuses a guest, whose player token names no account.
func signedIn(principal domain.Principal) error {
	if principal.AccountID == "" {
		return &GenerationError{Status: 403, Type: SignInRequiredProblemType,
			Detail: "sign in to use an account: a guest has none. Sign in with Google in the lobby, then get a new player token"}
	}
	return nil
}

// accountsUnavailable reports a failed read or write: the context's error
// for a cancelled request, and otherwise one that wraps
// domain.ErrUnavailable.
func accountsUnavailable(ctx context.Context, action string, err error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return fmt.Errorf("%w: %s: %w", domain.ErrUnavailable, action, err)
}
