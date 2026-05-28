package handlers

import (
	"context"
	"islandBattles/backend/dal"
)

type ConnectInput struct {
	RoomName     string
	ShouldCreate bool
}

type ConnectResult struct {
	UserID string
}

type ConnectResponse struct {
	Body struct {
		Result ConnectResult
	}
}

func (env *Env) Connect(ctx context.Context, input *ConnectInput) (*ConnectResponse, error) {
	game, err := dal.GetGameByRoomName(env.DB, input.RoomName)
	if err != nil {
		return nil, err
	}
	if input.ShouldCreate {
		if game == nil {

		}
		dal.CreateGame()
	}
	userID := "0"
	resp := &ConnectResponse{}
	resp.Body.Result = ConnectResult{UserID: userID}

	return resp, nil
}
