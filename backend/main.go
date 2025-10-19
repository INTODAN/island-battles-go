package main

import (
	"fmt"
	"islandBattles/backend/dependencies"
	"islandBattles/backend/handlers"
	"islandBattles/backend/routes"

	"github.com/gin-gonic/gin"
)

func main() {
	centrifugoClient := dependencies.ConnectCentrifugoClient()
	db := dependencies.InitDB()
	
	env := handlers.Env{CentrifugoClient: centrifugoClient, DB: db}
	
	r := gin.Default()

	routes.SetupRoutes(r, &env)

	fmt.Println("Listening on port 8888")

	r.Run(":8888")
	
	fmt.Println("Server closed")
}
