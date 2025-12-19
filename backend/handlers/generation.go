package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"islandBattles/backend/dal"
	"islandBattles/backend/dal/commandHandlers"
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
		GenerationID uint                   `json:"generation_id"`
		Commands     []models.CommandFields `json:"commands"`
	}
}

type NewGenerationResponse struct {
	Body struct {
		GenerationID uint `json:"generation_id"`
	}
}

func (env *Env) CreateGenerationFromTemplate(ctx context.Context, input *CreateGenerationFromTemplateRequest) (
	*NewGenerationResponse, error) {

	generationID, err := dal.CreateGenerationFromTemplate(env.DB, input.Body.TemplateID)
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
	increaseSoldiersAmountCommand := models.CommandFields{GenerationId: previousGeneration.ID,
		CommandType: models.IncreaseSoldierAmount,
		Parameters:  []byte("{}")}

	commands := append([]models.CommandFields{increaseSoldiersAmountCommand}, input.Body.Commands...)

	// 4. Iterate through the provided commands and apply their changes
	for _, cmd := range commands {
		err := processCommand(env, &cmd)
		if err != nil {
			fmt.Println("Error processing command:", err)
			return nil, err
		}
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

func processCommand(env *Env, command *models.CommandFields) error {
	var jsonParams any
	if err := json.Unmarshal(command.Parameters, &jsonParams); err != nil {
		fmt.Println("Error unmarshalling json parameters", err)
		return err
	}

	fmt.Println("Parsed params.")

	err := commandHandlers.Mapping[command.CommandType](env.DB, command.GenerationId, jsonParams)
	if err != nil {
		fmt.Println("Error while handling command in dal", err)
		return err
	}

	return nil
}
