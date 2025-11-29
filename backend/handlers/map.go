package handlers

import (
	"context"
	"fmt"
	"islandBattles/backend/dal"
	"islandBattles/backend/models"

	"github.com/danielgtaylor/huma/v2"
	"github.com/jackc/pgx/v5/pgtype"
)

type CreateMapTemplateResponse struct {
	Body struct {
		Result struct{MapTemplateId uint}
	}
}

type InputCastle struct {
	models.CastleFields
	RawPosition string `json:"position" example:"(x,y)"`
}

type CreateMapTemplateRequest struct {
	Body struct {
		models.MapFields
		Castles []InputCastle `json:"castles"`
	}
}

func (input *InputCastle) Resolve(ctx huma.Context, prefix *huma.PathBuffer) []error {
	var point pgtype.Point
	point.Scan(input.RawPosition)
	input.Position = point
	return nil
}

func (env *Env) CreateMapTemplate(ctx context.Context, input *CreateMapTemplateRequest) (*CreateMapTemplateResponse, error) {
	var castleTemplates []models.CastleTemplate

	for _, ct := range input.Body.Castles {
		castleTemplates = append(castleTemplates, models.CastleTemplate{
			CastleFields: ct.CastleFields,
		})
	}

	mapTemplateId, err := dal.CreateMapTemplate(env.DB, &models.MapTemplate{MapFields: input.Body.MapFields, CastleTemplates: castleTemplates})
	if err != nil {
		fmt.Println("Error when creating MapTemplate: ", err)
		return nil, huma.Error500InternalServerError(err.Error())
	}
	resp := &CreateMapTemplateResponse{}
	resp.Body.Result.MapTemplateId = mapTemplateId

	return resp, nil
}

type CreateMapFromTemplateRequest struct {
	Body struct {
		TemplateID uint `json:"template_id"`
	}
}

type CreateMapFromTemplateResponse struct {
	Body struct {
		Result struct {MapID uint}
	}
}

func (env *Env) CreateMapFromTemplate(ctx context.Context, input *CreateMapFromTemplateRequest) (*CreateMapFromTemplateResponse, error) {
	mapID, err := dal.CreateMapFromTemplate(env.DB, input.Body.TemplateID)
	if err != nil {
		fmt.Println("Error when creating Map from Template: ", err)
		return nil, huma.Error500InternalServerError(err.Error())
	}
	resp := &CreateMapFromTemplateResponse{}
	resp.Body.Result.MapID = mapID

	return resp, nil
}
