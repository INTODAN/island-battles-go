package handlers

import (
	"context"

	"islandBattles/backend/models"
	"islandBattles/backend/services"
)

// LeaderboardCacheControl lets CloudFront and browsers keep a leaderboard
// answer for a minute, so a visitor costs the database at most one read a
// minute per page.
const LeaderboardCacheControl = "public, max-age=60"

// Leaderboard serves the public leaderboard, without a credential, and the
// admin's routes that take a name off it, behind the admin API key.
type Leaderboard struct {
	Service services.Leaderboard
}

type LeaderboardResponse struct {
	CacheControl string `header:"Cache-Control"`
	Body         models.LeaderboardView
}

type LeaderboardCompetitorResponse struct {
	CacheControl string `header:"Cache-Control"`
	Body         models.LeaderboardCompetitorView
}

func (handler *Leaderboard) List(ctx context.Context, input *struct {
	Kind   string `query:"kind" enum:"bot,manual,house" doc:"Lists only competitors of this kind, ranked among themselves: bot, a registered bot name; manual, an account's hand play; house, a default bot level. Absent, every kind."`
	Limit  int    `query:"limit" minimum:"1" maximum:"100" default:"50" doc:"How many competitors to return, 1 to 100."`
	Offset int    `query:"offset" minimum:"0" default:"0" doc:"How many competitors to skip, in the leaderboard's order."`
}) (*LeaderboardResponse, error) {
	view, err := handler.Service.List(ctx, models.CompetitorKind(input.Kind), input.Limit, input.Offset)
	if err != nil {
		return nil, leaderboardHTTPError(ctx, err)
	}
	return &LeaderboardResponse{CacheControl: LeaderboardCacheControl, Body: view}, nil
}

func (handler *Leaderboard) Competitor(ctx context.Context, input *struct {
	CompetitorID string `path:"competitor_id" format:"uuid"`
}) (*LeaderboardCompetitorResponse, error) {
	view, err := handler.Service.Competitor(ctx, input.CompetitorID)
	if err != nil {
		return nil, leaderboardHTTPError(ctx, err)
	}
	return &LeaderboardCompetitorResponse{CacheControl: LeaderboardCacheControl, Body: view}, nil
}

func (handler *Leaderboard) RemoveBotName(ctx context.Context, input *struct {
	CompetitorID string `path:"competitor_id" format:"uuid"`
}) (*struct{}, error) {
	if err := handler.Service.RemoveBotName(ctx, input.CompetitorID); err != nil {
		return nil, leaderboardHTTPError(ctx, err)
	}
	return nil, nil
}

func (handler *Leaderboard) SetAccountHidden(ctx context.Context, input *struct {
	AccountID string `path:"account_id" format:"uuid"`
	Body      models.SetAccountHiddenRequest
}) (*struct{}, error) {
	if err := handler.Service.SetAccountHidden(ctx, input.AccountID, input.Body.Hidden); err != nil {
		return nil, leaderboardHTTPError(ctx, err)
	}
	return nil, nil
}

func leaderboardHTTPError(ctx context.Context, err error) error {
	return rejectionOrUnavailable(ctx, err, "the leaderboard is temporarily unavailable")
}
