package main

import (
	"fmt"
	"islandBattles/backend/config"
	"islandBattles/backend/routes"

	"github.com/gin-gonic/gin"
)

func main() {
	config.ConnectCentrifugoClient()
	config.ConnectDatabase()
	
	// defer config.Conn.Close(context.Background())
	
	r := gin.Default()

	routes.SetupRoutes(r)

	fmt.Println("Listening on port 8888")

	r.Run(":8888")
	
	fmt.Println("Server closed")
}
