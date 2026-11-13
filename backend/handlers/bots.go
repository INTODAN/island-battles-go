package handlers

import (
	"context"

	"islandBattles/backend/bots"
	"islandBattles/backend/models"
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
