package handlers

import (
	"context"

	"islandBattles/backend/bots"
	"islandBattles/backend/models"
)

type ListDefaultBotsResponse struct {
	Body struct {
		Bots []models.DefaultBotView `json:"bots" nullable:"false"`
	}
}

// ListDefaultBots lists the default bots that the backend plays itself. It
// reads no game, so it needs no database and no authentication.
func ListDefaultBots(context.Context, *struct{}) (*ListDefaultBotsResponse, error) {
	response := &ListDefaultBotsResponse{}
	response.Body.Bots = bots.Defaults()
	return response, nil
}
