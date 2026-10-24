package handlers

import (
	"context"
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
}

func (handler *LobbyGame) Replay(ctx context.Context, input *struct {
	LobbyID       string `path:"lobby_id" format:"uuid"`
	Authorization string `header:"Authorization"`
}) (*huma.StreamResponse, error) {
	verified, err := handler.Authenticator.Authenticate(ctx, input.Authorization)
	if err != nil {
		return nil, huma.Error401Unauthorized("authentication required")
	}
	service := services.GenerationService{Database: handler.Database}
	replay, err := service.Replay(ctx, verified.Principal, input.LobbyID)
	if err != nil {
		return nil, generationHTTPError(err)
	}
	if len(replay.Frames) == 0 {
		return nil, huma.Error404NotFound("game result is not ready")
	}
	html, err := services.RenderGameReplay(replay)
	if err != nil {
		return nil, huma.Error500InternalServerError("game result could not be rendered")
	}
	return &huma.StreamResponse{Body: func(response huma.Context) {
		response.SetHeader("Content-Type", "text/html; charset=utf-8")
		response.SetHeader("Cache-Control", "no-store")
		_, _ = response.BodyWriter().Write([]byte(html))
	}}, nil
}
