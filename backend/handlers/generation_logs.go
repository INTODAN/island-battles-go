package handlers

import (
	"context"

	"islandBattles/backend/models"
	"islandBattles/backend/services"
)

type GenerationLogsResponse struct{ Body models.GenerationLogs }

func (handler *LobbyGame) GenerationLogs(ctx context.Context, input *struct {
	LobbyID string `path:"lobby_id" format:"uuid"`
	// The service checks the generation after authorizing the caller, so a
	// caller off the roster learns nothing from a 422. SetupGameRoutes
	// documents the parameter as required.
	GenerationID  uint   `query:"generation_id" doc:"The generation whose lines to read, from Generation.id."`
	Authorization string `header:"Authorization"`
}) (*GenerationLogsResponse, error) {
	defer handler.runRetention(ctx)
	principal, err := handler.authenticate(ctx, input.Authorization)
	if err != nil {
		return nil, err
	}
	service := services.GenerationService{Database: handler.Database, Publisher: handler.Publisher, Bots: handler.Bots}
	logs, err := service.Logs(ctx, principal, input.LobbyID, input.GenerationID)
	if err != nil {
		return nil, generationHTTPError(ctx, err)
	}
	return &GenerationLogsResponse{Body: logs}, nil
}

// WatchLogs serves a generation's game log to the game's watchers: no lines
// while the game runs, and every team's once it has ended.
func (handler *LobbyGame) WatchLogs(ctx context.Context, input *struct {
	LobbyID string `path:"lobby_id" format:"uuid"`
	// Checked after authorization, as for GenerationLogs.
	GenerationID  uint   `query:"generation_id" doc:"The generation whose lines to read, from Generation.id."`
	Authorization string `header:"Authorization"`
}) (*GenerationLogsResponse, error) {
	defer handler.runRetention(ctx)
	principal, err := handler.authenticate(ctx, input.Authorization)
	if err != nil {
		return nil, err
	}
	service := services.GenerationService{Database: handler.Database, Publisher: handler.Publisher, Bots: handler.Bots, Watchers: handler.Watchers}
	logs, err := service.WatchLogs(ctx, principal, input.LobbyID, input.GenerationID)
	if err != nil {
		return nil, generationHTTPError(ctx, err)
	}
	return &GenerationLogsResponse{Body: logs}, nil
}
