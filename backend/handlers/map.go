package handlers

import (
	"context"
	"fmt"
	"islandBattles/backend/dal"
	"islandBattles/backend/models"

	"github.com/danielgtaylor/huma/v2"
	"github.com/jackc/pgx/v5/pgtype"
)

type CreateMapTemplateResult struct {
	MapTemplateId uint
}

type CreateMapTemplateResponse struct {
	Body struct {
		Result CreateMapTemplateResult
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
	castleTemplates := make([]models.CastleTemplate, len(input.Body.Castles))
	for i, c := range input.Body.Castles {
		castleTemplates[i] = models.CastleTemplate{
			CastleFields: c.CastleFields,
		}
	}

	mapTemplateId, err := dal.CreateMapTemplate(env.DB, &models.MapTemplate{MapFields: input.Body.MapFields, CastleTemplates: castleTemplates})
	if err != nil {
		fmt.Println("Error when creating MapTemplate: ", err)
		return nil, huma.Error500InternalServerError(err.Error())
	}
	resp := &CreateMapTemplateResponse{}
	resp.Body.Result = CreateMapTemplateResult{MapTemplateId: mapTemplateId}

	return resp, nil
}

