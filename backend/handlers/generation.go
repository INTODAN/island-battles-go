package handlers

import (
	"context"
	"islandBattles/backend/dal"
	"islandBattles/backend/models"
	"islandBattles/backend/services"

	"github.com/danielgtaylor/huma/v2"
)

type CreateGenerationFromTemplateRequest struct {
	Body struct {
		TemplateID uint `json:"template_id"`
		GameID     uint `json:"game_id"`
	}
}

type MigrateGenerationRequest struct {
	Body struct {
		GameID   uint                   `json:"game_id"`
		Commands []models.CommandFields `json:"commands"`
	}
}

type NewGenerationResponse struct {
	Body struct {
		GenerationID uint `json:"generation_id"`
	}
}

func (env *Env) CreateGenerationFromTemplate(ctx context.Context, input *CreateGenerationFromTemplateRequest) (
	*NewGenerationResponse, error) {

	generationID, err := dal.CreateGenerationFromTemplate(env.DB, input.Body.TemplateID, input.Body.GameID)
	if err != nil {
		return nil, huma.Error500InternalServerError(err.Error())
	}

	resp := &NewGenerationResponse{}
	resp.Body.GenerationID = generationID

	return resp, nil
}

// MigrateGeneration Takes the previous generation and creates the new generation.
// First, duplicates the map with a deep copy in the db.
// Then applies the default command, which is increasing the soldier group amount of each castle.
// Then, iterates through the commands, apply each of the command's changes.
func (env *Env) MigrateGeneration(ctx context.Context, input *MigrateGenerationRequest) (*NewGenerationResponse, error) {
	generationID, err := services.MigrateGeneration(env.DB, input.Body.GameID, input.Body.Commands)
	if err != nil {
		return nil, huma.Error500InternalServerError(err.Error())
	}

	resp := &NewGenerationResponse{}
	resp.Body.GenerationID = generationID

	return resp, nil
}
