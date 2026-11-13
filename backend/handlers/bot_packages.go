package handlers

import (
	"context"
	"errors"
	"net/http"

	"github.com/INTODAN/mpl/domain"
	"islandBattles/backend/models"
	"islandBattles/backend/observability"
	"islandBattles/backend/services"
	"islandBattles/backend/uploadtoken"

	"github.com/danielgtaylor/huma/v2"
)

// BotPackages serves the bot packages that members upload to a lobby.
type BotPackages struct {
	// Players tells who sends a request from its player token. Nil refuses
	// every request but those with an upload token.
	Players PlayerAuthenticator
	Service services.BotPackages
}

type BotPackageUploadResponse struct {
	Status int
	Body   models.BotPackageUploadView
}

type BotPackageResponse struct {
	Body models.BotPackageView
}

type BotPackagesResponse struct {
	Body models.BotPackagesView
}

type BotUploadTokenResponse struct {
	Body models.BotUploadTokenView
}

// principal authenticates a player's player token.
func (handler *BotPackages) principal(ctx context.Context, authorization string) (domain.Principal, error) {
	return authenticatePlayer(ctx, handler.Players, authorization)
}

// uploader authenticates a player's player token, or an upload token for the
// lobby, which stands for its player.
func (handler *BotPackages) uploader(ctx context.Context, authorization, lobbyID string) (domain.Principal, error) {
	token, isToken := uploadtoken.FromAuthorization(authorization)
	if !isToken {
		return handler.principal(ctx, authorization)
	}
	principal, err := handler.Service.TokenPrincipal(token, lobbyID)
	if err != nil {
		return domain.Principal{}, huma.Error401Unauthorized("the upload token is not valid for this lobby, or has expired; copy a new command from the bot picker")
	}
	return principal, nil
}

func (handler *BotPackages) Start(ctx context.Context, input *struct {
	LobbyID       string `path:"lobby_id" format:"uuid"`
	Authorization string `header:"Authorization"`
	Body          models.BotPackageUploadRequest
}) (*BotPackageUploadResponse, error) {
	principal, err := handler.uploader(ctx, input.Authorization, input.LobbyID)
	if err != nil {
		return nil, err
	}
	view, err := handler.Service.Start(ctx, principal, input.LobbyID, input.Body)
	if err != nil {
		return nil, botPackagesHTTPError(ctx, err)
	}
	return &BotPackageUploadResponse{Status: http.StatusCreated, Body: view}, nil
}

func (handler *BotPackages) Complete(ctx context.Context, input *struct {
	LobbyID       string `path:"lobby_id" format:"uuid"`
	PackageID     string `path:"package_id" format:"uuid"`
	Authorization string `header:"Authorization"`
}) (*BotPackageResponse, error) {
	principal, err := handler.uploader(ctx, input.Authorization, input.LobbyID)
	if err != nil {
		return nil, err
	}
	view, err := handler.Service.Complete(ctx, principal, input.LobbyID, input.PackageID)
	if err != nil {
		return nil, botPackagesHTTPError(ctx, err)
	}
	return &BotPackageResponse{Body: view}, nil
}

func (handler *BotPackages) List(ctx context.Context, input *struct {
	LobbyID       string `path:"lobby_id" format:"uuid"`
	Authorization string `header:"Authorization"`
}) (*BotPackagesResponse, error) {
	principal, err := handler.principal(ctx, input.Authorization)
	if err != nil {
		return nil, err
	}
	view, err := handler.Service.List(ctx, principal, input.LobbyID)
	if err != nil {
		return nil, botPackagesHTTPError(ctx, err)
	}
	return &BotPackagesResponse{Body: view}, nil
}

func (handler *BotPackages) Delete(ctx context.Context, input *struct {
	LobbyID       string `path:"lobby_id" format:"uuid"`
	PackageID     string `path:"package_id" format:"uuid"`
	Authorization string `header:"Authorization"`
}) (*struct{}, error) {
	principal, err := handler.principal(ctx, input.Authorization)
	if err != nil {
		return nil, err
	}
	if err := handler.Service.Delete(ctx, principal, input.LobbyID, input.PackageID); err != nil {
		return nil, botPackagesHTTPError(ctx, err)
	}
	return nil, nil
}

func (handler *BotPackages) UploadToken(ctx context.Context, input *struct {
	LobbyID       string `path:"lobby_id" format:"uuid"`
	Authorization string `header:"Authorization"`
}) (*BotUploadTokenResponse, error) {
	principal, err := handler.principal(ctx, input.Authorization)
	if err != nil {
		return nil, err
	}
	view, err := handler.Service.IssueUploadToken(ctx, principal, input.LobbyID)
	if err != nil {
		return nil, botPackagesHTTPError(ctx, err)
	}
	return &BotUploadTokenResponse{Body: view}, nil
}

// botPackagesHTTPError answers with the service's rejection, or the lobby's
// own answer for a caller who isn't an active member of the lobby.
func botPackagesHTTPError(ctx context.Context, err error) error {
	var rejection *services.GenerationError
	switch {
	case errors.As(err, &rejection):
		return &huma.ErrorModel{Type: rejection.Type, Title: http.StatusText(rejection.Status), Status: rejection.Status, Detail: rejection.Detail}
	case errors.Is(err, domain.ErrUnauthenticated):
		return huma.Error401Unauthorized("authentication required")
	case errors.Is(err, domain.ErrNotFound):
		return huma.Error404NotFound("lobby not found, or you are not one of its members")
	}
	observability.RecordError(ctx, err)
	return huma.Error503ServiceUnavailable("bot packages are temporarily unavailable")
}
