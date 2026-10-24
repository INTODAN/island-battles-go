package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"islandBattles/backend/demo"
	"islandBattles/backend/dependencies"
	"islandBattles/backend/handlers"
	"islandBattles/backend/lobbyadapter"
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
	"github.com/lmittmann/tint"
)

func main() {
	logger := slog.New(tint.NewHandler(os.Stdout, &tint.Options{Level: slog.LevelInfo}))
	config, err := dependencies.LoadConfig()
	if err != nil {
		logger.Error("Invalid configuration", "error", err)
		os.Exit(1)
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if err := run(ctx, config, logger); err != nil {
		logger.Error("Backend stopped", "error", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, config dependencies.Config, logger *slog.Logger) error {
	database, err := dependencies.InitDB(config.DatabaseURL)
	if err != nil {
		return err
	}
	sqlDatabase, err := database.DB()
	if err != nil {
		return err
	}
	defer func() { _ = sqlDatabase.Close() }()
	clock := domain.SystemClock{}
	identifiers := domain.CryptoIdentifiers{}
	policy := domain.DefaultPolicy("island-battles", "quiz-demo")
	// Requests push their own committed changes, so nothing runs between requests.
	publisher, err := realtime.NewPublisher(dependencies.ConnectCentrifugoClient(config), logger, realtime.DefaultPushRetry())
	if err != nil {
		return err
	}
	postgresStore, err := lobbypostgres.NewStore(database, clock)
	if err != nil {
		return err
	}
	store, err := application.NewPublishingStore(postgresStore, publisher)
	if err != nil {
		return err
	}
	identities, err := lobbypostgres.NewIdentityStore(database)
	if err != nil {
		return err
	}
	authentication, err := auth.NewService(identities, clock, policy, identifiers, []byte(config.ConnectionTokenHMACSecret), config.CentrifugoPublicURL)
	if err != nil {
		return err
	}
	lobbyService, err := application.NewService(store, clock, identifiers, policy)
	if err != nil {
		return err
	}
	gameBinding, err := lobbyadapter.NewService(database, lobbyService, publisher)
	if err != nil {
		return err
	}
	authorizer, err := realtime.NewAuthorizer(identities, store, clock, policy)
	if err != nil {
		return err
	}
	proxy, err := realtime.NewProxy(authorizer, config.LobbyProxySecret)
	if err != nil {
		return err
	}
	router := chi.NewMux()
	router.Use(httplog.RequestLogger(logger, &httplog.Options{Level: slog.LevelInfo, Schema: httplog.SchemaOTEL.Concise(true)}))
	api := humachi.New(router, routes.NewAPIConfig("Island Battles Go Backend", "1.0.0"))
	environment := handlers.Env{DB: database}
	routes.SetupRoutes(api, &environment)
	routes.SetupLobbyRoutes(api, lobbyService, authentication, gameBinding, &handlers.LobbyGame{Database: database, Authenticator: authentication, Publisher: publisher})
	proxy.Register(router)
	router.Get("/lobby/ui", func(response http.ResponseWriter, request *http.Request) {
		http.Redirect(response, request, "/lobby/ui/", http.StatusTemporaryRedirect)
	})
	lobbyFrontend, err := frontend.Handler([]frontend.Game{lobbyadapter.FrontendGame()})
	if err != nil {
		return err
	}
	router.Handle("/lobby/ui/*", http.StripPrefix("/lobby/ui/", lobbyFrontend))
	router.Handle("/game/demo/*", http.StripPrefix("/game/demo/", demo.Handler()))
	router.Get("/healthz", func(response http.ResponseWriter, request *http.Request) { response.WriteHeader(http.StatusNoContent) })
	server := &http.Server{Addr: config.HTTPAddr, Handler: router, ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 60 * time.Second}
	serverContext, stopServer := context.WithCancel(ctx)
	defer stopServer()
	shutdownDone := make(chan struct{})
	go func() {
		defer close(shutdownDone)
		<-serverContext.Done()
		shutdownContext, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownContext); err != nil {
			logger.Error("HTTP shutdown timed out", "error", err)
			_ = server.Close()
		}
	}()
	logger.Info("Listening", "address", config.HTTPAddr)
	serveError := server.ListenAndServe()
	stopServer()
	<-shutdownDone
	if serveError != nil && !errors.Is(serveError, http.ErrServerClosed) {
		return fmt.Errorf("serve HTTP: %w", serveError)
	}
	return nil
}
