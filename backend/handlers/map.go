package handlers

import (
	"context"
	"fmt"
	"islandBattles/backend/dal"
	"islandBattles/backend/models"

	"github.com/danielgtaylor/huma/v2"
)

type CreateMapTemplateResult struct {
	MapTemplateId uint
}

type CreateMapTemplateResponse struct {
	Body struct {
		Result CreateMapTemplateResult
	}
}

type CreateMapTemplateRequest struct {
	Body models.MapTemplateFields
}

func (env *Env) CreateMapTemplate(ctx context.Context, input *CreateMapTemplateRequest) (*CreateMapTemplateResponse, error) {
	MapTemplateId, err := dal.CreateMapTemplate(env.DB, &models.MapTemplate{MapTemplateFields: input.Body})
	if err != nil {
		fmt.Println("Error when creating MapTemplate: ", err)
		return nil, huma.Error500InternalServerError(err.Error())
	}
	resp := &CreateMapTemplateResponse{}
	resp.Body.Result = CreateMapTemplateResult{MapTemplateId: MapTemplateId}

	return resp, nil
}

