package handlers

import (
	"context"
	"errors"
	"net/http"

	"islandBattles/backend/models"
	"islandBattles/backend/observability"
	"islandBattles/backend/services"
	"islandBattles/lobby/domain"

	"github.com/danielgtaylor/huma/v2"
)

// LobbyBots serves the bot choices of a waiting lobby's members.
type LobbyBots struct {
	Authenticator domain.Authenticator
	Service       services.LobbyBots
}

type LobbyBotsResponse struct {
	Body models.LobbyBotsView
}

func (handler *LobbyBots) Choose(ctx context.Context, input *struct {
	LobbyID       string `path:"lobby_id" format:"uuid"`
	Authorization string `header:"Authorization"`
	Body          models.BotChoiceView
}) (*LobbyBotsResponse, error) {
	verified, err := handler.Authenticator.Authenticate(ctx, input.Authorization)
	if err != nil {
		return nil, huma.Error401Unauthorized("authentication required")
	}
	view, err := handler.Service.Choose(ctx, verified.Principal, input.LobbyID, input.Body)
	if err != nil {
		return nil, lobbyBotsHTTPError(ctx, err)
	}
	return &LobbyBotsResponse{Body: view}, nil
}

func (handler *LobbyBots) List(ctx context.Context, input *struct {
	LobbyID       string `path:"lobby_id" format:"uuid"`
	Authorization string `header:"Authorization"`
}) (*LobbyBotsResponse, error) {
	verified, err := handler.Authenticator.Authenticate(ctx, input.Authorization)
	if err != nil {
		return nil, huma.Error401Unauthorized("authentication required")
	}
	view, err := handler.Service.List(ctx, verified.Principal, input.LobbyID)
	if err != nil {
		return nil, lobbyBotsHTTPError(ctx, err)
	}
	return &LobbyBotsResponse{Body: view}, nil
}

// lobbyBotsHTTPError answers with the service's rejection, or the lobby's
// own answer for a caller who isn't a member of the lobby.
func lobbyBotsHTTPError(ctx context.Context, err error) error {
	var rejection *services.GenerationError
	switch {
	case errors.As(err, &rejection):
		return &huma.ErrorModel{Type: rejection.Type, Title: http.StatusText(rejection.Status), Status: rejection.Status, Detail: rejection.Detail}
	case errors.Is(err, domain.ErrUnauthenticated):
		return huma.Error401Unauthorized("authentication required")
	case errors.Is(err, domain.ErrNotFound):
		return huma.Error404NotFound("lobby not found")
	}
	observability.RecordError(ctx, err)
	return huma.Error503ServiceUnavailable("bot choices are temporarily unavailable")
}
