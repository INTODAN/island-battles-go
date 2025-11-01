package routes

import (
	"islandBattles/backend/handlers"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
)


func SetupRoutes(api *huma.API, env *handlers.Env) {
	huma.Register(*api, huma.Operation{Path: "/ping", Method: http.MethodPost, DefaultStatus: http.StatusOK}, env.Ping)
	huma.Register(*api, huma.Operation{Path: "/connect", Method: http.MethodPost, DefaultStatus: http.StatusOK}, env.Connect)
	huma.Register(*api, huma.Operation{Path: "/castle", Method: http.MethodPost, DefaultStatus: http.StatusOK}, env.CreateCastle)
}