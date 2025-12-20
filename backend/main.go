package main

import (
	"fmt"
	"islandBattles/backend/dependencies"
	"islandBattles/backend/handlers"
	"islandBattles/backend/routes"
	"log/slog"
	"net/http"
	"os"

	"github.com/lmittmann/tint"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/httplog/v3"
)

func main() {
	logger := slog.New(tint.NewHandler(os.Stdout, &tint.Options{
		Level: slog.LevelInfo,
	}))

	centrifugoClient := dependencies.ConnectCentrifugoClient()
	db := dependencies.InitDB(dependencies.DatabaseURL)

	router := chi.NewMux()
	router.Use(httplog.RequestLogger(logger, &httplog.Options{
		Level:  slog.LevelInfo,
		Schema: httplog.SchemaOTEL.Concise(true),
	}))
	api := humachi.New(router, huma.DefaultConfig("Island Battles Go Backend", "1.0.0"))

	env := handlers.Env{CentrifugoClient: centrifugoClient, DB: db}

	routes.SetupRoutes(&api, &env)

	fmt.Println("Listening on port 8888")

	if err := http.ListenAndServe(":8888", router); err != nil {
		logger.Error("Server failed to listen and serve", "error", err)
		os.Exit(1)
	}

	fmt.Println("Server closed")
}
