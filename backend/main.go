package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/centrifugal/gocent/v3"
	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"
)

type DirectPingResponse struct {
	Message string `json:"message" example:"direct_pong" doc:"Direct ping response with pong."`
}

type RPCResponse struct {
	Result struct {
		Data json.RawMessage
	}
}

type HTTPRPCResponse struct {
	Body RPCResponse
}

const centrifugoAddress = "http://localhost:8000/api"

const centrifugoApiKey = "0Bmi8sRKc-lpnA77ti0AW_P8vWIn8FDgfVkknlv-xIclvAlIX0DGIC95bRy_K6PXvP1Yb-J3m2mTAd2rVoTLnQ"

func main() {
	// Create a new router & API
	router := chi.NewMux()
	api := humachi.New(router, huma.DefaultConfig("My API", "1.0.0"))

	config := gocent.Config{Addr: centrifugoAddress, Key: centrifugoApiKey}

	client := gocent.New(config)

	huma.Post(api, "/direct_ping", func(ctx context.Context, input *struct{}) (*HTTPRPCResponse, error) {
		fmt.Println("received new ping request!")
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()

		centrifugoResponseJson, err := json.Marshal(DirectPingResponse{Message: "direct_pong"})

		if err != nil {
			fmt.Println(err.Error())
			return nil, err
		}

		_, err = client.Publish(ctx, "direct_ping", centrifugoResponseJson)
		if err != nil {
			fmt.Println("Error on publish: ", err)
			return nil, err
		}

		resp := RPCResponse{Result: struct{ Data json.RawMessage }{Data: json.RawMessage(`{"message": "direct_pong"}`)}}

		httpResponse := &HTTPRPCResponse{Body: resp}
		fmt.Println("Returning response!")
		return httpResponse, nil
	})

	fmt.Println("Listening on port 8888")

	err := http.ListenAndServe("127.0.0.1:8888", router)
	if err != nil {
		fmt.Println(err)
		return
	}
}
