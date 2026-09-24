package handlers

import (
	"context"
	"errors"
	"islandBattles/backend/dal"
	"islandBattles/backend/services"
	"islandBattles/lobby/domain"

	"github.com/danielgtaylor/huma/v2"
	"gorm.io/gorm"
)

// LobbyGame serves saved spectator results only to current lobby members.
type LobbyGame struct {
	Database      *gorm.DB
	Lobbies       domain.SnapshotReader
	Authenticator domain.Authenticator
}

func (handler *LobbyGame) Replay(ctx context.Context, input *struct {
	LobbyID       string `path:"lobby_id" format:"uuid"`
	Authorization string `header:"Authorization"`
}) (*huma.StreamResponse, error) {
	verified, err := handler.Authenticator.Authenticate(ctx, input.Authorization)
	if err != nil {
		return nil, huma.Error401Unauthorized("authentication required")
	}
	snapshot, err := handler.Lobbies.Snapshot(ctx, verified.Principal, domain.Scope{GameKey: "island-battles", LobbyID: input.LobbyID})
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return nil, huma.Error404NotFound("lobby not found")
		}
		return nil, huma.Error503ServiceUnavailable("lobby is temporarily unavailable")
	}
	if snapshot.Lobby.Phase != domain.PhaseStarted {
		return nil, huma.Error409Conflict("lobby has not started")
	}
	replay, err := dal.GetLobbyGameReplay(handler.Database.WithContext(ctx), input.LobbyID)
	if err != nil {
		return nil, huma.Error503ServiceUnavailable("game result is temporarily unavailable")
	}
	if len(replay) == 0 {
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
