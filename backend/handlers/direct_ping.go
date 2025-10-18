package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"islandBattles/backend/config"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)


type DirectPingResponse struct {
	Message string `json:"message" example:"direct_pong" doc:"Direct ping response with pong."`
}

type RPCResponse struct {
	Result struct {
		Data json.RawMessage
	}
}

func DirectPing(c *gin.Context) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	centrifugoResponseJson, err := json.Marshal(DirectPingResponse{Message: "direct_pong"})

	if err != nil {
		fmt.Println(err.Error())
		return
	}

	_, err = config.Client.Publish(ctx, "direct_ping", centrifugoResponseJson)
	if err != nil {
		fmt.Println("Error on publish: ", err)
		return
	}

	resp := RPCResponse{Result: struct{ Data json.RawMessage }{Data: json.RawMessage(`{"message": "direct_pong"}`)}}

	c.JSON(http.StatusOK, resp)
}
