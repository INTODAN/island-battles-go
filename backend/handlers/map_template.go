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
	MapTemplateID uint
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
	if err := point.Scan(input.RawPosition); err != nil {
		return []error{fmt.Errorf("invalid position format: %w", err)}
	}
	input.Position = point
	return nil
}

func (env *Env) CreateMapTemplate(ctx context.Context, input *CreateMapTemplateRequest) (*CreateMapTemplateResponse, error) {
	var castleTemplates []models.CastleTemplate

	for i, inputCastle := range input.Body.Castles {
		castleTemplate := models.CastleTemplate{
			CastleFields: inputCastle.CastleFields,
		}
		castleTemplate.CastleID = uint(i)
		castleTemplates = append(castleTemplates, castleTemplate)
	}

	mapTemplateID, err := dal.CreateMapTemplate(env.DB, &models.MapTemplate{MapFields: input.Body.MapFields, CastleTemplates: castleTemplates})
	if err != nil {
		fmt.Println("Error when creating MapTemplate: ", err)
		return nil, huma.Error500InternalServerError(err.Error())
	}
	resp := &CreateMapTemplateResponse{}
	resp.Body.Result = CreateMapTemplateResult{MapTemplateID: mapTemplateID}

	return resp, nil
}
