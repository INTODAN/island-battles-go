package handlers

import (
	"context"
	"fmt"
	"islandBattles/backend/services"

	"github.com/danielgtaylor/huma/v2"
	"github.com/go-chi/httplog/v3"
)

func (env *Env) GetGameGraph(ctx context.Context, input *struct {
	GameID uint `path:"id" doc:"The ID of the game"`
}) (*huma.StreamResponse, error) {
	htmlContent, err := services.GetGameGraph(env.DB, input.GameID)
	if err != nil {
		return nil, huma.Error500InternalServerError("Get game graph failed", err)
	}

	return &huma.StreamResponse{
		Body: func(ctx huma.Context) {
			ctx.SetHeader("Content-Type", "text/html")
			// The status is already sent, so the request's log line carries the error.
			if _, err := ctx.BodyWriter().Write([]byte(htmlContent)); err != nil {
				_ = httplog.SetError(ctx.Context(), fmt.Errorf("write game graph: %w", err))
			}
		},
	}, nil
}
