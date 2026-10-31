package handlers

import (
	"context"
	"fmt"
	"islandBattles/backend/observability"
	"islandBattles/backend/services"
	"islandBattles/lobby/domain"

	"github.com/danielgtaylor/huma/v2"
	"gorm.io/gorm"
)

// LobbyGame serves a started game to its frozen roster without reading the lobby.
type LobbyGame struct {
	Database      *gorm.DB
	Authenticator domain.Authenticator
	Publisher     services.Publisher
	// Retention runs at the end of every game request: turns, which are what
	// adds game data, and the state and replay reads that players poll. Nil
	// deletes nothing. retention.Retention is the real one; it has the same
	// shape as the lobby's runner but deletes only game data.
	Retention domain.RetentionRunner
}

// runRetention runs after the handler has its response, and after a turn has
// committed and pushed its events.
func (handler *LobbyGame) runRetention(ctx context.Context) {
	if handler.Retention != nil {
		handler.Retention.RunIfDue(ctx)
	}
}

func (handler *LobbyGame) Replay(ctx context.Context, input *struct {
	LobbyID       string `path:"lobby_id" format:"uuid"`
	Authorization string `header:"Authorization"`
}) (*huma.StreamResponse, error) {
	defer handler.runRetention(ctx)
	verified, err := handler.Authenticator.Authenticate(ctx, input.Authorization)
	if err != nil {
		return nil, huma.Error401Unauthorized("authentication required")
	}
	service := services.GenerationService{Database: handler.Database}
	replay, err := service.Replay(ctx, verified.Principal, input.LobbyID)
	if err != nil {
		return nil, generationHTTPError(ctx, err)
	}
	if len(replay.Frames) == 0 {
		return nil, huma.Error404NotFound("game result is not ready")
	}
	html, err := services.RenderGameReplay(replay)
	if err != nil {
		observability.RecordError(ctx, fmt.Errorf("render replay: %w", err))
		return nil, huma.Error500InternalServerError("game result could not be rendered")
	}
	return &huma.StreamResponse{Body: func(response huma.Context) {
		response.SetHeader("Content-Type", "text/html; charset=utf-8")
		response.SetHeader("Cache-Control", "no-store")
		_, _ = response.BodyWriter().Write([]byte(html))
	}}, nil
}

func (handler *LobbyGame) Demo(ctx context.Context, input *struct {
	LobbyID       string `path:"lobby_id" format:"uuid"`
	Authorization string `header:"Authorization"`
}) (*huma.StreamResponse, error) {
	defer handler.runRetention(ctx)
	verified, err := handler.Authenticator.Authenticate(ctx, input.Authorization)
	if err != nil {
		return nil, huma.Error401Unauthorized("authentication required")
	}
	service := services.GenerationService{Database: handler.Database}
	demo, err := service.Demo(ctx, verified.Principal, input.LobbyID)
	if err != nil {
		return nil, generationHTTPError(ctx, err)
	}
	if len(demo.Frames) == 0 {
		return nil, huma.Error404NotFound("game is not ready")
	}
	return &huma.StreamResponse{Body: func(response huma.Context) {
		response.SetHeader("Content-Type", "application/json")
		response.SetHeader("Content-Disposition", fmt.Sprintf("attachment; filename=%q", services.DemoFileName(demo)))
		response.SetHeader("Cache-Control", "no-store")
		// The status went out with the first bytes, so a failed write can
		// only cut the body short. A player who leaves cancels the request,
		// which is not an error.
		if err := services.WriteDemo(response.BodyWriter(), demo); err != nil && ctx.Err() == nil {
			observability.RecordError(ctx, fmt.Errorf("write demo: %w", err))
		}
	}}, nil
}
