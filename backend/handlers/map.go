package handlers

import (
	"context"
	"fmt"
	"islandBattles/backend/dal"

	"github.com/danielgtaylor/huma/v2"
)

type CreateMapFromTemplateRequest struct {
	Body struct {
		TemplateID uint `json:"template_id"`
	}
}

type CreateMapFromTemplateResult struct {
	MapID uint
}

type CreateMapFromTemplateResponse struct {
	Body struct {
		Result CreateMapFromTemplateResult
	}
}

func (env *Env) CreateMapFromTemplate(ctx context.Context, input *CreateMapFromTemplateRequest) (*CreateMapFromTemplateResponse, error) {
	mapID, err := dal.CreateMapFromTemplate(env.DB, input.Body.TemplateID)
	if err != nil {
		fmt.Println("Error when creating Map from Template: ", err)
		return nil, huma.Error500InternalServerError(err.Error())
	}
	resp := &CreateMapFromTemplateResponse{}
	resp.Body.Result = CreateMapFromTemplateResult{MapID: mapID}

	return resp, nil
}
