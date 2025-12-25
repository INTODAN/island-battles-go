package handlers

import (
	"context"
	"fmt"
	"islandBattles/backend/services"

	"github.com/danielgtaylor/huma/v2"
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
			_, err := ctx.BodyWriter().Write([]byte(htmlContent))
			if err != nil {
				fmt.Printf("error encountered while writing %v", err)
				return
			}
		},
	}, nil
}
