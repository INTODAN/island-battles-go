package handlers

import (
	"context"
	"errors"
	"github.com/danielgtaylor/huma/v2"
	"gorm.io/gorm"
	"islandBattles/backend/models"
	"islandBattles/backend/services"
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
	verified, err := handler.Authenticator.Authenticate(ctx, input.Authorization)
	if err != nil {
		return nil, huma.Error401Unauthorized("authentication required")
	}
	service := services.GenerationService{Database: handler.Database, Lobbies: handler.Lobbies}
	state, err := service.Current(ctx, verified.Principal, input.LobbyID)
	if err != nil {
		return nil, generationHTTPError(err)
	}
	return &GenerationStateResponse{Body: state}, nil
}

func (handler *LobbyGame) RegisterGenerationCommands(ctx context.Context, input *RegisterGenerationCommandsRequest) (*GenerationStateResponse, error) {
	verified, err := handler.Authenticator.Authenticate(ctx, input.Authorization)
	if err != nil {
		return nil, huma.Error401Unauthorized("authentication required")
	}
	service := services.GenerationService{Database: handler.Database, Lobbies: handler.Lobbies}
	state, err := service.Register(ctx, verified.Principal, input.Body)
	if err != nil {
		return nil, generationHTTPError(err)
	}
	return &GenerationStateResponse{Body: state}, nil
}

func generationHTTPError(err error) error {
	var rejection *services.GenerationError
	if errors.As(err, &rejection) {
		return huma.NewError(rejection.Status, rejection.Detail)
	}
	if errors.Is(err, domain.ErrNotFound) {
		return huma.Error404NotFound("game not found")
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return huma.Error409Conflict("game generation is not initialized")
	}
	return huma.Error503ServiceUnavailable("game is temporarily unavailable")
}
