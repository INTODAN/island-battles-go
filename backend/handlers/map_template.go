package handlers

import (
	"context"
	"errors"
	"fmt"
	"islandBattles/backend/dal"
	"islandBattles/backend/models"
	"islandBattles/backend/observability"
	"islandBattles/lobby/domain"

	"github.com/danielgtaylor/huma/v2"
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
		observability.RecordError(ctx, fmt.Errorf("create map template: %w", err))
		return nil, huma.Error500InternalServerError(err.Error())
	}
	resp := &CreateMapTemplateResponse{}
	resp.Body.MapTemplateID = mapTemplateID
	return resp, nil
}

type ValidateMapTemplateRequest struct {
	// RawBody keeps the exact bytes, so the check is the strict decoding that
	// reading a map file uses rather than Huma's body parsing.
	RawBody []byte `contentType:"application/json"`
}

// MapTemplateValidation is the result of checking a map file.
type MapTemplateValidation struct {
	Valid  bool                `json:"valid" doc:"Whether the map file passes every check."`
	Errors []*huma.ErrorDetail `json:"errors" nullable:"false" doc:"One entry per problem, located relative to the map file, such as castles[2].x. A problem with the whole file has no location."`
}

type ValidateMapTemplateResponse struct {
	Body MapTemplateValidation
}

// ValidateMapTemplate checks a map file with models.DecodeMapTemplate and
// stores nothing. An invalid map is a successful check, so it answers 200 and
// lists the problems.
func ValidateMapTemplate(_ context.Context, input *ValidateMapTemplateRequest) (*ValidateMapTemplateResponse, error) {
	response := &ValidateMapTemplateResponse{Body: MapTemplateValidation{Valid: true, Errors: []*huma.ErrorDetail{}}}
	_, err := models.DecodeMapTemplate(input.RawBody)
	var invalidTemplate *models.MapTemplateError
	switch {
	case errors.As(err, &invalidTemplate):
		response.Body.Valid = false
		response.Body.Errors = invalidTemplate.Details
	case err != nil:
		return nil, err
	}
	return response, nil
}

type ListMapTemplatesResponse struct {
	Body struct {
		Maps []models.MapTemplateSummaryView `json:"maps" nullable:"false"`
	}
}

// ListMapTemplates lists the default maps that a lobby can start on: maps
// whose team colours are exactly 1 to player_amount, made for a team count
// that a lobby allows.
func (env *Env) ListMapTemplates(ctx context.Context, _ *struct{}) (*ListMapTemplatesResponse, error) {
	templates, err := dal.ListMapTemplates(env.DB.WithContext(ctx))
	if err != nil {
		observability.RecordError(ctx, fmt.Errorf("list map templates: %w", err))
		return nil, huma.Error500InternalServerError("could not list default maps")
	}
	response := &ListMapTemplatesResponse{}
	response.Body.Maps = []models.MapTemplateSummaryView{}
	for _, template := range templates {
		if summary, ok := template.Summary(); ok && summary.TeamCount >= domain.MinTeams && summary.TeamCount <= domain.MaxTeams {
			response.Body.Maps = append(response.Body.Maps, summary)
		}
	}
	return response, nil
}
