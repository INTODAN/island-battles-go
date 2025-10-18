package routes

import (
	"islandBattles/backend/handlers"

	"github.com/gin-gonic/gin"
)


func SetupRoutes(router *gin.Engine) {
	router.POST("direct_ping", handlers.DirectPing)
}