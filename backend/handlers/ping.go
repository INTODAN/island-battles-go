package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/danielgtaylor/huma/v2"
)

type PingResponse struct {
	Message string `json:"message" example:"direct_pong" doc:"Direct ping response with pong."`
}

type RPCResponse struct {
	Body struct {
		Result struct {
			Data json.RawMessage
		}
	}
}

func (env *Env) Ping(ctx context.Context, input *struct{}) (*RPCResponse, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	centrifugoResponseJson, err := json.Marshal(PingResponse{Message: "pong"})

	if err != nil {
		fmt.Println(err.Error())
		return nil, huma.Error500InternalServerError(err.Error())
	}

	_, err = env.CentrifugoClient.Publish(ctx, "ping", centrifugoResponseJson)
	if err != nil {
		fmt.Println("Error off publish: ", err)
		return nil, huma.Error500InternalServerError(err.Error())
	}

	resp := &RPCResponse{}
	resp.Body.Result = struct{ Data json.RawMessage }{Data: json.RawMessage(`{}`)}

	return resp, nil
}
