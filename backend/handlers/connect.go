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
	userId := "0"
	resp := &ConnectResponse{}
	resp.Body.Result = ConnectResult{User: userId}

	return resp, nil
}
