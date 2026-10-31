package handlers

import (
	"context"

	"github.com/danielgtaylor/huma/v2"
	"islandBattles/backend/models"
	"islandBattles/backend/services"
)

type GenerationLogsResponse struct{ Body models.GenerationLogs }

func (handler *LobbyGame) GenerationLogs(ctx context.Context, input *struct {
	LobbyID string `path:"lobby_id" format:"uuid"`
	// The handler checks the generation after authentication, so a caller
	// without credentials learns nothing from a 422. SetupLobbyRoutes
	// documents the parameter as required.
	GenerationID  uint   `query:"generation_id" doc:"The generation whose lines to read, from Generation.id."`
	Authorization string `header:"Authorization"`
}) (*GenerationLogsResponse, error) {
	defer handler.runRetention(ctx)
	verified, err := handler.Authenticator.Authenticate(ctx, input.Authorization)
	if err != nil {
		return nil, huma.Error401Unauthorized("authentication required")
	}
	if input.GenerationID == 0 {
		return nil, huma.Error422UnprocessableEntity("generation_id must be a positive generation ID")
	}
	service := services.GenerationService{Database: handler.Database}
	logs, err := service.Logs(ctx, verified.Principal, input.LobbyID, input.GenerationID)
	if err != nil {
		return nil, generationHTTPError(ctx, err)
	}
	return &GenerationLogsResponse{Body: logs}, nil
}
