package handlers

import (
	"context"
	"errors"
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

// MigrateGeneration plays one turn of the game with the given commands and
// stores the result as the next generation; see services.MigrateGeneration.
func (env *Env) MigrateGeneration(ctx context.Context, input *MigrateGenerationRequest) (*NewGenerationResponse, error) {
	generationID, err := services.MigrateGeneration(env.DB.WithContext(ctx), input.Body.GameID, input.Body.Commands)
	if err != nil {
		if errors.Is(err, models.ErrInvalidUpgrade) {
			return nil, huma.Error422UnprocessableEntity(err.Error())
		}
		if errors.Is(err, models.ErrUpgradeMaxLevel) || errors.Is(err, models.ErrInsufficientSoldiers) {
			return nil, huma.Error409Conflict(err.Error())
		}
		return nil, huma.Error500InternalServerError(err.Error())
	}

	resp := &NewGenerationResponse{}
	resp.Body.GenerationID = generationID

	return resp, nil
}
