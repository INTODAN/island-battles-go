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
	user_id := "0"
	resp := &ConnectResponse{}
	resp.Body.Result = ConnectResult{User: user_id}

	return resp, nil
}
