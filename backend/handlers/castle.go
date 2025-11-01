package handlers

import (
	"context"
	"fmt"
	"islandBattles/backend/dal"
	"islandBattles/backend/models"

	"github.com/danielgtaylor/huma/v2"
)

type CreateCastleResult struct {
	CastleId uint `json:"castle_id"`
}

type CreateCastleResponse struct {
	Body struct {
		Result CreateCastleResult
	}
}

type CreateCastleRequest struct {
	User uint `json:"user_id"`
}

func (env *Env) CreateCastle(ctx context.Context, input *CreateCastleRequest) (*CreateCastleResponse, error) {
	var castleFields models.CastleFields

	castleId, err := dal.CreateCastle(env.DB, models.Castle{CastleFields: models.CastleFields{TeamColor: castleFields.TeamColor, Position: castleFields.Position, SoldierAmount: castleFields.SoldierAmount}})
	if err != nil {
		fmt.Println("Error when creating castle: ", err)
		return nil, huma.Error500InternalServerError(err.Error())
	}
	resp := &CreateCastleResponse{}
	resp.Body.Result = CreateCastleResult{CastleId: castleId}

	return resp, nil
}
