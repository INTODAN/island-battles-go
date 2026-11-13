// Package app builds the backend's HTTP handler without hosting it. Two hosts
// serve it: the HTTP server in backend/, for local development, and the AWS
// Lambda function in backend/cmd/lambda. Neither runs schema changes; the
// migrate command in backend/cmd/migrate does, once per deploy.
package app

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"islandBattles/backend/demo"
	"islandBattles/backend/demoviewer"
	"islandBattles/backend/dependencies"
	"islandBattles/backend/handlers"
	"islandBattles/backend/lobbyadapter"
	"islandBattles/backend/mapbuilder"
	"islandBattles/backend/observability"
	"islandBattles/backend/packagestore"
	"islandBattles/backend/retention"
	"islandBattles/backend/routes"
	"islandBattles/backend/services"
	"islandBattles/backend/submittoken"
	"islandBattles/backend/uploadtoken"
	"islandBattles/lobby/application"
	"islandBattles/lobby/auth"
	"islandBattles/lobby/domain"
	"islandBattles/lobby/frontend"
	lobbypostgres "islandBattles/lobby/postgres"
	"islandBattles/lobby/realtime"

	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"
	"gorm.io/gorm"
)

// Option adds what only some hosts serve.
type Option func(*options)

type options struct {
	gameGraph bool
	// localPackages keeps bot packages when no bucket is configured, and
	// localPackagesHandler serves its storage at localPackagesPrefix.
	localPackages        packagestore.Store
	localPackagesPrefix  string
	localPackagesHandler http.Handler
}

// WithGameGraph serves GET /game/{id}/graph, which reads any game without
// authentication. Only the local server host passes it.
func WithGameGraph() Option {
	return func(options *options) { options.gameGraph = true }
}

// WithLocalBotPackages keeps bot packages in store when the configuration
// names no bucket, and serves handler, the store's S3-compatible server, at
// prefix. Only the local server host passes it.
func WithLocalBotPackages(store packagestore.Store, prefix string, handler http.Handler) Option {
	return func(options *options) {
		options.localPackages, options.localPackagesPrefix, options.localPackagesHandler = store, prefix, handler
	}
}

// New wires the stores and services to a router. It starts no goroutines and
// holds nothing that outlives a request besides the database pool, which the
// host owns and closes. Without options it serves only what is safe in public.
func New(config dependencies.Config, database *gorm.DB, logger *slog.Logger, opts ...Option) (http.Handler, error) {
	var selected options
	for _, option := range opts {
		option(&selected)
	}
	clock := domain.SystemClock{}
	identifiers := domain.CryptoIdentifiers{}
	policy := domain.DefaultPolicy(
		lobbyadapter.Registration(database),
		// quiz-demo only shows that a second game can share the lobby. With no
		// game behind it to read settings, it accepts any object. It has no
		// seats, so it refuses every seat the host tries to add.
		domain.GameRegistration{Key: "quiz-demo", ValidateSettings: func(context.Context, string, json.RawMessage, []domain.TeamDefinition) error { return nil }},
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
	retentionStore, err := lobbypostgres.NewRetentionStore(database)
	if err != nil {
		return nil, err
	}
	// Lobby requests delete expired lobby data in small batches as they end.
	lobbyRetention, err := application.NewRetention(retentionStore, clock, policy, logger)
	if err != nil {
		return nil, err
	}
	authorizer, err := realtime.NewAuthorizer(identities, store, clock, policy)
	if err != nil {
		return nil, err
	}
	// The proxy answers an outage with a bare 503, so its cause goes on the
	// request's error line.
	proxy, err := realtime.NewProxy(authorizer, config.LobbyProxySecret, realtime.WithErrorReporter(observability.RecordError))
	if err != nil {
		return nil, err
	}
	// Game requests delete expired games in small batches as they end.
	gameRetentionPolicy := retention.DefaultPolicy()
	gameRetentionPolicy.Period = config.GameRetentionPeriod
	gameRetention, err := retention.New(retention.DatabaseStore{Database: database}, gameRetentionPolicy, time.Now, logger)
	if err != nil {
		return nil, err
	}

	submitTokens, err := submittoken.New([]byte(config.SubmitTokenSecret))
	if err != nil {
		return nil, err
	}
	// Upload tokens share the submit tokens' secret; their MAC covers their
	// own prefix, so neither kind passes for the other.
	uploadTokens, err := uploadtoken.New([]byte(config.SubmitTokenSecret))
	if err != nil {
		return nil, err
	}
	var packages packagestore.Store
	switch {
	case config.BotPackagesBucket != "":
		bucket, err := packagestore.FromEnvironment(context.Background(), config.BotPackagesBucket)
		if err != nil {
			return nil, err
		}
		packages = bucket
	case selected.localPackages != nil:
		packages = selected.localPackages
	default:
		logger.Warn("Bot package uploads answer 503 because " + dependencies.BotPackagesBucketEnv + " is not set")
	}

	// The authenticated player goes on a failed request's error line.
	authenticated := annotatingAuthentication{authentication}

	router := chi.NewMux()
	router.Use(observability.Requests(logger))
	api := humachi.New(router, routes.NewAPIConfig("Island Battles Go Backend", "1.0.0"))
	// Before any operation is registered, so every operation gets it.
	api.UseMiddleware(observability.NameOperations)
	environment := handlers.Env{DB: database}
	routes.SetupRoutes(api, &environment, config.AdminAPIKey)
	if config.AdminAPIKey == "" {
		logger.Warn("POST /map/create refuses every request because " + dependencies.AdminAPIKeyEnv + " is not set")
	}
	if selected.gameGraph {
		routes.SetupGameGraphRoute(api, &environment)
	}
	routes.SetupLobbyRoutes(api, lobbyService, authenticated, gameBinding, lobbyRetention, &handlers.LobbyGame{Database: database, Authenticator: authenticated, Publisher: publisher, Retention: gameRetention, Watchers: lobbyService,
		SubmitTokens: submitTokens, PublicURL: config.PublicURL})
	routes.SetupBotPackageRoutes(api, &handlers.BotPackages{Authenticator: authenticated, Service: services.BotPackages{
		Database: database, Lobbies: lobbyService, GameKey: lobbyadapter.GameKey, Store: packages, Tokens: uploadTokens}})
	if config.BotPackagesBucket == "" && selected.localPackagesHandler != nil {
		router.Handle(selected.localPackagesPrefix+"/*", selected.localPackagesHandler)
	}
	proxy.Register(router)
	if err := registerPages(router); err != nil {
		return nil, err
	}
	router.Get("/healthz", healthz(Version))
	return router, nil
}

// annotatingAuthentication adds the player of each verified session to the
// request's error line.
type annotatingAuthentication struct{ *auth.Service }

func (authentication annotatingAuthentication) Authenticate(ctx context.Context, authorization string) (domain.SessionVerification, error) {
	verified, err := authentication.Service.Authenticate(ctx, authorization)
	switch {
	case err == nil:
		observability.Annotate(ctx, slog.String("player_id", verified.Principal.PlayerID))
	case !errors.Is(err, domain.ErrUnauthenticated):
		// The lobby's authentication answers with its own problem, which
		// leaves the cause out, such as an unreachable database.
		observability.RecordError(ctx, err)
	}
	return verified, err
}

// registerPages serves the embedded browser pages, which need no files beside
// the binary. The bare name opens the lobby.
func registerPages(router chi.Router) error {
	toLobby := func(response http.ResponseWriter, request *http.Request) {
		http.Redirect(response, request, "/lobby/ui/", http.StatusTemporaryRedirect)
	}
	router.Get("/", toLobby)
	router.Get("/lobby/ui", toLobby)
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
	demoViewer, err := demoviewer.Handler()
	if err != nil {
		return err
	}
	router.Get("/game/demo-viewer", func(response http.ResponseWriter, request *http.Request) {
		http.Redirect(response, request, "/game/demo-viewer/", http.StatusTemporaryRedirect)
	})
	router.Handle("/game/demo-viewer/*", http.StripPrefix("/game/demo-viewer/", demoViewer))
	return nil
}
