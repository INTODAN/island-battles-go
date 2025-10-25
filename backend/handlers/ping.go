package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)


type PingResponse struct {
	Message string `json:"message" example:"direct_pong" doc:"Direct ping response with pong."`
}

type RPCResponse struct {
	Result struct {
		Data json.RawMessage
	}
}

func (env *Env) Ping(c *gin.Context) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	centrifugoResponseJson, err := json.Marshal(PingResponse{Message: "pong"})

	if err != nil {
		fmt.Println(err.Error())
		return
	}

	_, err = env.CentrifugoClient.Publish(ctx, "ping", centrifugoResponseJson)
	if err != nil {
		fmt.Println("Error on publish: ", err)
		return
	}

	resp := RPCResponse{Result: struct{ Data json.RawMessage }{Data: json.RawMessage(`{}`)}}

	c.JSON(http.StatusOK, resp)
}
