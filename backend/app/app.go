// Package app builds the backend's HTTP handler without hosting it. Two hosts
// serve it: the HTTP server in backend/, for local development, and the AWS
// Lambda function in backend/cmd/lambda. Neither runs schema changes; the
// migrate command in backend/cmd/migrate does, once per deploy.
package app

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"

	"islandBattles/backend/demo"
	"islandBattles/backend/dependencies"
	"islandBattles/backend/handlers"
	"islandBattles/backend/lobbyadapter"
	"islandBattles/backend/mapbuilder"
	"islandBattles/backend/routes"
	"islandBattles/lobby/application"
	"islandBattles/lobby/auth"
	"islandBattles/lobby/domain"
	"islandBattles/lobby/frontend"
	lobbypostgres "islandBattles/lobby/postgres"
	"islandBattles/lobby/realtime"

	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/httplog/v3"
	"gorm.io/gorm"
)

// New wires the stores and services to a router. It starts no goroutines and
// holds nothing that outlives a request besides the database pool, which the
// host owns and closes.
func New(config dependencies.Config, database *gorm.DB, logger *slog.Logger) (http.Handler, error) {
	clock := domain.SystemClock{}
	identifiers := domain.CryptoIdentifiers{}
	policy := domain.DefaultPolicy(
		lobbyadapter.Registration(database),
		// quiz-demo only shows that a second game can share the lobby. With no
		// game behind it to read settings, it accepts any object.
		domain.GameRegistration{Key: "quiz-demo", ValidateSettings: func(context.Context, json.RawMessage, []domain.TeamDefinition) error { return nil }},
	)
	// Requests push their own committed changes, so nothing runs between requests.
	publisher, err := realtime.NewPublisher(dependencies.ConnectCentrifugoClient(config), logger, realtime.DefaultPushRetry())
	if err != nil {
		return nil, err
	}
	postgresStore, err := lobbypostgres.NewStore(database, clock)
	if err != nil {
		return nil, err
	}
	store, err := application.NewPublishingStore(postgresStore, publisher)
	if err != nil {
		return nil, err
	}
	identities, err := lobbypostgres.NewIdentityStore(database)
	if err != nil {
		return nil, err
	}
	authentication, err := auth.NewService(identities, clock, policy, identifiers, []byte(config.ConnectionTokenHMACSecret), config.CentrifugoPublicURL)
	if err != nil {
		return nil, err
	}
	lobbyService, err := application.NewService(store, clock, identifiers, policy)
	if err != nil {
		return nil, err
	}
	gameBinding, err := lobbyadapter.NewService(database, lobbyService, publisher)
	if err != nil {
		return nil, err
	}
	authorizer, err := realtime.NewAuthorizer(identities, store, clock, policy)
	if err != nil {
		return nil, err
	}
	proxy, err := realtime.NewProxy(authorizer, config.LobbyProxySecret)
	if err != nil {
		return nil, err
	}

	router := chi.NewMux()
	router.Use(httplog.RequestLogger(logger, &httplog.Options{Level: slog.LevelInfo, Schema: httplog.SchemaOTEL.Concise(true)}))
	api := humachi.New(router, routes.NewAPIConfig("Island Battles Go Backend", "1.0.0"))
	environment := handlers.Env{DB: database}
	routes.SetupRoutes(api, &environment)
	routes.SetupLobbyRoutes(api, lobbyService, authentication, gameBinding, &handlers.LobbyGame{Database: database, Authenticator: authentication, Publisher: publisher})
	proxy.Register(router)
	if err := registerPages(router); err != nil {
		return nil, err
	}
	router.Get("/healthz", func(response http.ResponseWriter, request *http.Request) { response.WriteHeader(http.StatusNoContent) })
	return router, nil
}

// registerPages serves the embedded browser pages, which need no files beside
// the binary.
func registerPages(router chi.Router) error {
	router.Get("/lobby/ui", func(response http.ResponseWriter, request *http.Request) {
		http.Redirect(response, request, "/lobby/ui/", http.StatusTemporaryRedirect)
	})
	lobbyFrontend, err := frontend.Handler([]frontend.Game{lobbyadapter.FrontendGame()})
	if err != nil {
		return err
	}
	router.Handle("/lobby/ui/*", http.StripPrefix("/lobby/ui/", lobbyFrontend))
	router.Handle("/game/demo/*", http.StripPrefix("/game/demo/", demo.Handler()))
	mapBuilder, err := mapbuilder.Handler()
	if err != nil {
		return err
	}
	router.Get("/map/builder", func(response http.ResponseWriter, request *http.Request) {
		http.Redirect(response, request, "/map/builder/", http.StatusTemporaryRedirect)
	})
	router.Handle("/map/builder/*", http.StripPrefix("/map/builder/", mapBuilder))
	return nil
}
