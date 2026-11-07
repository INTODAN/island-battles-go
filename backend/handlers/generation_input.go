package handlers

import (
	"context"
	"errors"
	"github.com/danielgtaylor/huma/v2"
	"gorm.io/gorm"
	"islandBattles/backend/models"
	"islandBattles/backend/observability"
	"islandBattles/backend/services"
	"islandBattles/lobby/domain"
	"log/slog"
	"net/http"
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
	verified, err := handler.Authenticator.Authenticate(ctx, input.Authorization)
	if err != nil {
		return nil, huma.Error401Unauthorized("authentication required")
	}
	observability.Annotate(ctx, slog.Uint64("game_id", uint64(input.Body.GameID)), slog.Uint64("generation_id", uint64(input.Body.GenerationID)))
	service := services.GenerationService{Database: handler.Database, Publisher: handler.Publisher}
	state, err := service.Register(ctx, verified.Principal, input.Body)
	if err != nil {
		return nil, generationHTTPError(ctx, err)
	}
	return &GenerationStateResponse{Body: state}, nil
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
