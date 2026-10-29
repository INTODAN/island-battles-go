package handlers

import (
	"context"
	"errors"
	"fmt"
	"islandBattles/backend/observability"
	"islandBattles/backend/services"

	"github.com/danielgtaylor/huma/v2"
	"gorm.io/gorm"
)

func (env *Env) GetGameGraph(ctx context.Context, input *struct {
	GameID uint `path:"id" doc:"The ID of the game"`
}) (*huma.StreamResponse, error) {
	htmlContent, err := services.GetGameGraph(env.DB, input.GameID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, huma.Error404NotFound("game not found")
	}
	if err != nil {
		return nil, huma.Error500InternalServerError("Get game graph failed", err)
	}

	return &huma.StreamResponse{
		Body: func(ctx huma.Context) {
			ctx.SetHeader("Content-Type", "text/html")
			// The status is already sent, so the error only reaches the request's
			// error line if it fails for another reason.
			if _, err := ctx.BodyWriter().Write([]byte(htmlContent)); err != nil {
				observability.RecordError(ctx.Context(), fmt.Errorf("write game graph: %w", err))
			}
		},
	}, nil
}
