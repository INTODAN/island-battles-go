package main

import (
	"fmt"
	"islandBattles/backend/dependencies"
	"islandBattles/backend/handlers"
	"islandBattles/backend/routes"
	"net/http"

	"github.com/danielgtaylor/huma/middleware"
	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
)

func main() {
	centrifugoClient := dependencies.ConnectCentrifugoClient()
	db := dependencies.InitDB()
	
	router := http.NewServeMux()
	
	api := humago.New(router, huma.DefaultConfig("Island Battles Go Backend", "1.0.0"))
	
	
	env := handlers.Env{CentrifugoClient: centrifugoClient, DB: db}
	
	routes.SetupRoutes(&api, &env)

	fmt.Println("Listening on port 8888")
	
	handler := middleware.Logger(router)

	http.ListenAndServe("127.0.0.1:8888", handler)

	
	fmt.Println("Server closed")
}
