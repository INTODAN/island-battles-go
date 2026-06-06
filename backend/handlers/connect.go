package handlers

import (
	"context"
	"islandBattles/backend/dal"
)

type ConnectAction string

const (
	Create ConnectAction = "create"
	Join   ConnectAction = "join"
)

type ConnectInput struct {
	RoomName      string
	ConnectAction ConnectAction
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
	if input.ConnectAction == Create {
		_, err := dal.CreateGame(env.DB)
		if err != nil {
			return nil, err
		}
	} else if input.ConnectAction == Join {
		_, err := dal.GetGameByRoomName(env.DB, input.RoomName)
		if err != nil {
			return nil, err
		}
	}
	userID := "0"
	resp := &ConnectResponse{}
	resp.Body.Result = ConnectResult{UserID: userID}

	return resp, nil
}
