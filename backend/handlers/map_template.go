package handlers

import (
	"context"
	"errors"
	"fmt"
	"islandBattles/backend/dal"
	"islandBattles/backend/models"

	"github.com/danielgtaylor/huma/v2"
	"github.com/go-chi/httplog/v3"
)

type CreateMapTemplateRequest struct {
	Body models.MapTemplateView
}

type CreateMapTemplateResponse struct {
	Body struct {
		MapTemplateID uint `json:"map_template_id"`
	}
}

func (env *Env) CreateMapTemplate(ctx context.Context, input *CreateMapTemplateRequest) (*CreateMapTemplateResponse, error) {
	mapTemplateID, err := dal.CreateMapTemplate(env.DB, input.Body)
	var invalidTemplate *models.MapTemplateError
	switch {
	case errors.As(err, &invalidTemplate):
		return nil, huma.Error422UnprocessableEntity("invalid map template", invalidTemplate.Located("body")...)
	case errors.Is(err, dal.ErrMapTemplateNameTaken):
		return nil, huma.Error409Conflict("map name is already taken", &huma.ErrorDetail{Location: "body.map_name", Value: input.Body.MapName, Message: "another map template uses this name"})
	case err != nil:
		_ = httplog.SetError(ctx, fmt.Errorf("create map template: %w", err))
		return nil, huma.Error500InternalServerError(err.Error())
	}
	resp := &CreateMapTemplateResponse{}
	resp.Body.MapTemplateID = mapTemplateID
	return resp, nil
}
