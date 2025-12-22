package handlers

import (
	"context"
)

type ConnectResult struct {
	User string
}

type ConnectResponse struct {
	Body struct {
		Result ConnectResult
	}
}

func (env *Env) Connect(ctx context.Context, input *struct{}) (*ConnectResponse, error) {
	userID := "0"
	resp := &ConnectResponse{}
	resp.Body.Result = ConnectResult{User: userID}

	return resp, nil
}
