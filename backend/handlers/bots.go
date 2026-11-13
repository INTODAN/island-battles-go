package handlers

import (
	"context"

	"islandBattles/backend/bots"
	"islandBattles/backend/models"
	"islandBattles/backend/observability"

	"github.com/danielgtaylor/huma/v2"
)

type ListDefaultBotsResponse struct {
	Body struct {
		Bots       []models.DefaultBotView `json:"bots" nullable:"false"`
		ServerBots bool                    `json:"server_bots" doc:"Whether this server offers the bots that it plays without a player's tab: kind hosted, a player's own bot server at a URL, and kind uploaded, a bot package uploaded to the lobby, as a player's own bot in their member settings and for a bot seat. When false, only the localhost bot and the default bots can be chosen."`
	}
}

// ListDefaultBots lists the default bots that the backend plays itself, and
// whether the server offers hosted and uploaded bots. It reads no game, so it
// needs no database and no authentication.
func (env *Env) ListDefaultBots(context.Context, *struct{}) (*ListDefaultBotsResponse, error) {
	response := &ListDefaultBotsResponse{}
	response.Body.Bots = bots.Defaults()
	response.Body.ServerBots = env.ServerBots
	return response, nil
}

type GetBotComputeResponse struct {
	Body models.BotComputeView
}

// GetBotCompute reports the bot runners' compute this month against their
// budget, through the guard's cache, so that the bot pickers can grey out
// uploaded bots once it is spent. It needs no authentication. A failed read
// answers 503, and its cause goes on the request's error line.
func (env *Env) GetBotCompute(ctx context.Context, _ *struct{}) (*GetBotComputeResponse, error) {
	view, err := env.BotCompute.Usage(ctx)
	if err != nil {
		observability.RecordError(ctx, err)
		return nil, huma.Error503ServiceUnavailable("the bot runners' compute is temporarily unavailable")
	}
	return &GetBotComputeResponse{Body: view}, nil
}
