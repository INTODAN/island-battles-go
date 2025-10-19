package routes

import (
	"islandBattles/backend/handlers"

	"github.com/gin-gonic/gin"
)


func SetupRoutes(router *gin.Engine, env *handlers.Env) {
	router.POST("direct_ping", env.DirectPing)
}