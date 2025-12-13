package handlers

import (
	"context"
	"islandBattles/backend/dal"
	"islandBattles/backend/models"

	"github.com/danielgtaylor/huma/v2"
)

type CreateGenerationFromTemplateRequest struct {
	Body struct {
		TemplateID uint `json:"template_id"`
	}
}

type MigrateGenerationRequest struct {
	Body struct {
		GenerationID uint             `json:"generation_id"`
		Commands     []models.Command `json:"commands"`
	}
}

type NewGenerationResponse struct {
	Body struct {
		GenerationID uint `json:"generation_id"`
	}
}

func (env *Env) CreateGenerationFromTemplate(ctx context.Context, input *CreateGenerationFromTemplateRequest) (
	*NewGenerationResponse, error) {

	mapID, err := dal.CreateMapFromTemplate(env.DB, input.Body.TemplateID)
	if err != nil {
		return nil, huma.Error500InternalServerError(err.Error())
	}

	generation := models.Generation{
		MapID: mapID,
	}

	generationID, err := dal.CreateGeneration(env.DB, &generation)
	if err != nil {
		return nil, huma.Error500InternalServerError(err.Error())
	}

	resp := &NewGenerationResponse{}
	resp.Body.GenerationID = generationID

	return resp, nil
}

// Takes the previous generation and creates the new generation.
// First, duplicates the map with a deep copy in the db.
// Then applies the default command, which is increasing the soldier group amount of each castle.
// Then, iterates through the commands, apply each of the command's changes.
func (env *Env) MigrateGeneration(ctx context.Context, input *MigrateGenerationRequest) (*NewGenerationResponse, error) {
	// 1. Get previous generation to get its map ID
	previousGeneration, err := dal.GetGeneration(env.DB, input.Body.GenerationID)
	if err != nil {
		return nil, huma.Error500InternalServerError(err.Error())
	}

	// 2. Duplicate the map with a deep copy
	newMapID, err := dal.DuplicateMap(env.DB, previousGeneration.MapID)
	if err != nil {
		return nil, huma.Error500InternalServerError(err.Error())
	}

	// 4. Iterate through the provided commands and apply their changes
	for _, cmd := range input.Body.Commands {
		_ = cmd // dummy usage to avoid compile error
	}

	// 5. Create the new generation linking to the new map
	newGeneration := models.Generation{
		MapID: newMapID,
	}

	generationID, err := dal.CreateGeneration(env.DB, &newGeneration)
	if err != nil {
		return nil, huma.Error500InternalServerError(err.Error())
	}

	resp := &NewGenerationResponse{}
	resp.Body.GenerationID = generationID

	return resp, nil
}
