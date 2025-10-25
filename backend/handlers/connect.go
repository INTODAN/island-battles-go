package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

type ConnectResponse struct {
	Result struct {
		User string `json:"user"`
	}
}

func (env *Env) Connect(c *gin.Context) {
	user_id := "0"
	resp := ConnectResponse{Result: struct {
		User string `json:"user"`
	}{User: user_id}}

	c.JSON(http.StatusOK, resp)
}
