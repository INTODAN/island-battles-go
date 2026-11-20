package handlers

import (
	"context"
	"errors"
	"net/http"

	"github.com/INTODAN/mpl/domain"
	"islandBattles/backend/models"
	"islandBattles/backend/observability"
	"islandBattles/backend/services"

	"github.com/danielgtaylor/huma/v2"
)

// Accounts serves a signed-in player's account: their hand play and the bot
// names they register. A guest's player token names no account, and gets
// 403.
type Accounts struct {
	// Players tells who sends a request from its player token. Nil refuses
	// every request.
	Players PlayerAuthenticator
	Service services.Accounts
}

type IslandAccountResponse struct {
	Body models.IslandAccountView
}

type CompetitorResponse struct {
	Status int
	Body   models.CompetitorView
}

func (handler *Accounts) Read(ctx context.Context, input *struct {
	Authorization string `header:"Authorization"`
}) (*IslandAccountResponse, error) {
	principal, err := authenticatePlayer(ctx, handler.Players, input.Authorization)
	if err != nil {
		return nil, err
	}
	view, err := handler.Service.Read(ctx, principal)
	if err != nil {
		return nil, accountsHTTPError(ctx, err)
	}
	return &IslandAccountResponse{Body: view}, nil
}

func (handler *Accounts) RegisterBotName(ctx context.Context, input *struct {
	Authorization string `header:"Authorization"`
	Body          models.RegisterBotNameRequest
}) (*CompetitorResponse, error) {
	principal, err := authenticatePlayer(ctx, handler.Players, input.Authorization)
	if err != nil {
		return nil, err
	}
	view, err := handler.Service.RegisterBotName(ctx, principal, input.Body.Name)
	if err != nil {
		return nil, accountsHTTPError(ctx, err)
	}
	return &CompetitorResponse{Status: http.StatusCreated, Body: view}, nil
}

func (handler *Accounts) DeleteBotName(ctx context.Context, input *struct {
	CompetitorID  string `path:"competitor_id" format:"uuid"`
	Authorization string `header:"Authorization"`
}) (*struct{}, error) {
	principal, err := authenticatePlayer(ctx, handler.Players, input.Authorization)
	if err != nil {
		return nil, err
	}
	if err := handler.Service.DeleteBotName(ctx, principal, input.CompetitorID); err != nil {
		return nil, accountsHTTPError(ctx, err)
	}
	return nil, nil
}

func (handler *Accounts) Delete(ctx context.Context, input *struct {
	Authorization string `header:"Authorization"`
}) (*struct{}, error) {
	principal, err := authenticatePlayer(ctx, handler.Players, input.Authorization)
	if err != nil {
		return nil, err
	}
	if err := handler.Service.Delete(ctx, principal); err != nil {
		return nil, accountsHTTPError(ctx, err)
	}
	return nil, nil
}

// accountsHTTPError answers with the service's rejection, and with 503 when
// the database failed, whose cause goes on the request's error line.
func accountsHTTPError(ctx context.Context, err error) error {
	var rejection *services.GenerationError
	if errors.As(err, &rejection) {
		return &huma.ErrorModel{Type: rejection.Type, Title: http.StatusText(rejection.Status), Status: rejection.Status, Detail: rejection.Detail}
	}
	if !errors.Is(err, domain.ErrUnavailable) && ctx.Err() == nil {
		err = errors.Join(domain.ErrUnavailable, err)
	}
	observability.RecordError(ctx, err)
	return huma.Error503ServiceUnavailable("accounts are temporarily unavailable")
}
