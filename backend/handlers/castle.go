package handlers

import (
	"context"
	"fmt"
	"islandBattles/backend/dal"
	"islandBattles/backend/models"

	"github.com/danielgtaylor/huma/v2"
	"github.com/jackc/pgx/v5/pgtype"
)

type CreateCastleResult struct {
	CastleId uint
}

type CreateCastleResponse struct {
	Body struct {
		Result CreateCastleResult
	}
}

type CreateCastleBody struct {
	UserId uint
	models.CastleFields
	Position pgtype.Vec2
}

type CreateCastleRequest struct {
	Body CreateCastleBody
}

func (env *Env) CreateCastle(ctx context.Context, input *CreateCastleRequest) (*CreateCastleResponse, error) {
	fmt.Println("Received position: ", input.Body.Position)
	castle := models.Castle{CastleFields: models.CastleFields{TeamColor: input.Body.TeamColor, Position: pgtype.Point{P: input.Body.Position, Valid: true}, SoldierAmount: input.Body.SoldierAmount}}
	castleId, err := dal.CreateCastle(env.DB, &castle)
	if err != nil {
		fmt.Println("Error when creating castle: ", err)
		return nil, huma.Error500InternalServerError(err.Error())
	}
	resp := &CreateCastleResponse{}
	resp.Body.Result = CreateCastleResult{CastleId: castleId}

	return resp, nil
}
