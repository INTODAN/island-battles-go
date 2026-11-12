package handlers

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"gorm.io/gorm"
	"islandBattles/backend/models"
	"islandBattles/backend/observability"
	"islandBattles/backend/services"
	"islandBattles/backend/submittoken"
	"islandBattles/lobby/domain"
)

type GenerationStateResponse struct{ Body models.GenerationState }
type RegisterGenerationCommandsRequest struct {
	Authorization string `header:"Authorization"`
	Body          models.RegisterGenerationCommands
}

func (handler *LobbyGame) CurrentGeneration(ctx context.Context, input *struct {
	LobbyID       string `path:"lobby_id" format:"uuid"`
	Authorization string `header:"Authorization"`
}) (*GenerationStateResponse, error) {
	defer handler.runRetention(ctx)
	verified, err := handler.Authenticator.Authenticate(ctx, input.Authorization)
	if err != nil {
		return nil, huma.Error401Unauthorized("authentication required")
	}
	service := services.GenerationService{Database: handler.Database, Publisher: handler.Publisher}
	state, err := service.Current(ctx, verified.Principal, input.LobbyID)
	if err != nil {
		return nil, generationHTTPError(ctx, err)
	}
	return &GenerationStateResponse{Body: state}, nil
}

// ApplyTurnDeadline applies the open turn's deadline once it has passed and
// answers the caller's state, as CurrentGeneration does.
func (handler *LobbyGame) ApplyTurnDeadline(ctx context.Context, input *struct {
	LobbyID       string `path:"lobby_id" format:"uuid"`
	Authorization string `header:"Authorization"`
}) (*GenerationStateResponse, error) {
	defer handler.runRetention(ctx)
	verified, err := handler.Authenticator.Authenticate(ctx, input.Authorization)
	if err != nil {
		return nil, huma.Error401Unauthorized("authentication required")
	}
	service := services.GenerationService{Database: handler.Database, Publisher: handler.Publisher}
	state, err := service.ApplyDeadline(ctx, verified.Principal, input.LobbyID)
	if err != nil {
		return nil, generationHTTPError(ctx, err)
	}
	return &GenerationStateResponse{Body: state}, nil
}

func (handler *LobbyGame) RegisterGenerationCommands(ctx context.Context, input *RegisterGenerationCommandsRequest) (*GenerationStateResponse, error) {
	defer handler.runRetention(ctx)
	service := services.GenerationService{Database: handler.Database, Publisher: handler.Publisher}
	var principal domain.Principal
	if token, found := submittoken.FromAuthorization(input.Authorization); found {
		// A submit token stands for its player, for its game and generation
		// only.
		var err error
		if principal, err = handler.verifySubmitToken(ctx, &service, token, input.Body); err != nil {
			return nil, err
		}
	} else {
		verified, err := handler.Authenticator.Authenticate(ctx, input.Authorization)
		if err != nil {
			return nil, huma.Error401Unauthorized("authentication required")
		}
		principal = verified.Principal
	}
	observability.Annotate(ctx, slog.Uint64("game_id", uint64(input.Body.GameID)), slog.Uint64("generation_id", uint64(input.Body.GenerationID)))
	state, err := service.Register(ctx, principal, input.Body)
	if err != nil {
		return nil, generationHTTPError(ctx, err)
	}
	return &GenerationStateResponse{Body: state}, nil
}

// verifySubmitToken returns the player a submit token stands for, when the
// token verifies and names the batch's game and generation and a player on
// that game's roster. Anything else is 401.
func (handler *LobbyGame) verifySubmitToken(ctx context.Context, service *services.GenerationService, token string, batch models.RegisterGenerationCommands) (domain.Principal, error) {
	refused := huma.Error401Unauthorized(submittoken.ErrInvalid.Error())
	if handler.SubmitTokens == nil {
		return domain.Principal{}, refused
	}
	claims, err := handler.SubmitTokens.Verify(token, time.Now())
	if err != nil || claims.GameID != batch.GameID || claims.GenerationID != batch.GenerationID {
		return domain.Principal{}, refused
	}
	principal, err := service.AuthorizeSubmitToken(ctx, claims)
	if errors.Is(err, submittoken.ErrInvalid) {
		return domain.Principal{}, refused
	}
	if err != nil {
		return domain.Principal{}, generationHTTPError(ctx, err)
	}
	observability.Annotate(ctx, slog.String("player_id", principal.PlayerID))
	return principal, nil
}

type TurnDeliveryResponse struct{ Body models.TurnDelivery }

// TurnDeliveryRequest reads the request's own address too, for the submit
// URL when no public URL is configured.
type TurnDeliveryRequest struct {
	LobbyID       string `path:"lobby_id" format:"uuid"`
	Authorization string `header:"Authorization"`
	origin        string
}

// Resolve keeps the scheme and Host the request came in with.
func (request *TurnDeliveryRequest) Resolve(ctx huma.Context) []error {
	scheme := "http"
	if ctx.TLS() != nil {
		scheme = "https"
	}
	request.origin = scheme + "://" + ctx.Host()
	return nil
}

// TurnDelivery answers the caller's turn of the open generation with a
// submit token, for their bot to play and submit itself.
func (handler *LobbyGame) TurnDelivery(ctx context.Context, input *TurnDeliveryRequest) (*TurnDeliveryResponse, error) {
	defer handler.runRetention(ctx)
	verified, err := handler.Authenticator.Authenticate(ctx, input.Authorization)
	if err != nil {
		return nil, huma.Error401Unauthorized("authentication required")
	}
	if handler.SubmitTokens == nil {
		return nil, huma.Error503ServiceUnavailable("turn deliveries are not configured")
	}
	origin := strings.TrimRight(handler.PublicURL, "/")
	if origin == "" {
		origin = input.origin
	}
	service := services.GenerationService{Database: handler.Database, Publisher: handler.Publisher}
	delivery, err := service.TurnDelivery(ctx, verified.Principal, input.LobbyID, handler.SubmitTokens, origin+"/generation/commands")
	if err != nil {
		return nil, generationHTTPError(ctx, err)
	}
	return &TurnDeliveryResponse{Body: delivery}, nil
}

// generationHTTPError answers a service error. An unexpected one answers 503
// without its cause, which goes on the request's error line instead.
func generationHTTPError(ctx context.Context, err error) error {
	var rejection *services.GenerationError
	if errors.As(err, &rejection) {
		return &huma.ErrorModel{Type: rejection.Type, Title: http.StatusText(rejection.Status), Status: rejection.Status, Detail: rejection.Detail}
	}
	if errors.Is(err, domain.ErrNotFound) {
		return huma.Error404NotFound("game not found")
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return huma.Error409Conflict("game generation is not initialized")
	}
	observability.RecordError(ctx, err)
	return huma.Error503ServiceUnavailable("game is temporarily unavailable")
}
