// Package app builds the backend's HTTP handler without hosting it. Two hosts
// serve it: the HTTP server in backend/, for local development, and the AWS
// Lambda function in backend/cmd/lambda. Neither runs schema changes; the
// migrate command in backend/cmd/migrate does, once per deploy.
package app

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"islandBattles/backend/botrunner"
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
	lobbyapp "islandBattles/lobby/app"
	"islandBattles/lobby/gamehooks"
	"islandBattles/lobby/playertoken"
	"islandBattles/lobby/realtime"

	"github.com/centrifugal/gocent/v3"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"
	"gorm.io/gorm"
)

// Option adds what only some hosts serve.
type Option func(*options)

type options struct {
	gameGraph bool
	// localRunners play hosted and uploaded bots when no runner functions
	// are configured, and localOrigin is where the bots submit when no
	// public URL is.
	localRunners services.BotRunners
	localOrigin  string
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

// WithLocalBotRunners plays hosted and uploaded bots' turns with runners when
// the configuration names no runner functions, has the bots submit to origin
// when it names no public URL, and accepts hosted bots on this machine.
// Only the local server host passes it, with the runners in its own process.
func WithLocalBotRunners(runners services.BotRunners, origin string) Option {
	return func(options *options) { options.localRunners, options.localOrigin = runners, origin }
}

// New wires the stores and services to a router. It starts no goroutines and
// holds nothing that outlives a request besides the database pool, which the
// host owns and closes. Without options it serves only what is safe in public.
//
// Without config.LobbyURL, the lobby runs in this process, built by lobby/app
// as the lobby's own hosts build it, and New serves its routes too. With it,
// the lobby is a server of its own (lobby/cmd/lobby): New serves none of the
// lobby's routes, reads the lobby's game API over the network, and verifies
// player tokens with the lobby's JWKS. Either way the game serves its hooks.
func New(config dependencies.Config, database *gorm.DB, logger *slog.Logger, opts ...Option) (http.Handler, error) {
	var selected options
	for _, option := range opts {
		option(&selected)
	}
	centrifugo := dependencies.ConnectCentrifugoClient(config)
	// Requests push their own committed changes, so nothing runs between requests.
	publisher, err := realtime.NewPublisher(centrifugo, logger, realtime.DefaultPushRetry())
	if err != nil {
		return nil, err
	}
	gamePublisher := publications{publisher}
	// The lobby and the game talk HTTP. While the lobby runs in this process,
	// the lobby's hook calls and the game's game API calls reach the other
	// side's handler in memory, through loopback; the hooks are set below.
	var hooks http.Handler
	loopback := &Loopback{}
	var lobby *lobbyapp.Lobby
	var playerKeys playertoken.KeySource
	gameAPIURL := config.LobbyURL
	if config.LobbyURL == "" {
		lobby, err = inProcessLobby(config, database, centrifugo, logger, loopback, func() http.Handler { return hooks })
		if err != nil {
			return nil, err
		}
		gameAPIURL = LoopbackOrigin
		playerKeys = lobby.PlayerTokenKeys()
	} else {
		playerKeys, err = playertoken.NewJWKSSource(config.LobbyURL + playertoken.JWKSPath)
		if err != nil {
			return nil, err
		}
	}
	lobbyClient, err := gamehooks.NewGameAPIClient(lobbyadapter.GameKey, gameAPIURL, config.LobbyHookSecret, gamehooks.WithHTTPClient(loopback.Client()))
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
	bots, err := turnSender(config, selected, packages, submitTokens, logger)
	if err != nil {
		return nil, err
	}
	serverBots := services.ServerBots{Enabled: config.ServerBots, LoopbackHosts: selected.localRunners != nil}
	hooks, err = lobbyadapter.Hooks(config.LobbyHookSecret, lobbyadapter.Registration(database, gamePublisher, bots, serverBots, lobbyClient))
	if err != nil {
		return nil, err
	}
	// The game verifies player tokens with the lobby's keys: in this
	// process while the lobby runs here, and otherwise from its JWKS.
	playerVerifier, err := playertoken.NewVerifier(config.LobbyIssuer(), playerKeys)
	if err != nil {
		return nil, err
	}

	router := chi.NewMux()
	router.Use(observability.Requests(logger))
	api := humachi.New(router, routes.NewAPIConfig("Island Battles Go Backend", "1.0.0"))
	// Before any operation is registered, so every operation gets it.
	api.UseMiddleware(observability.NameOperations)
	environment := handlers.Env{DB: database, ServerBots: config.ServerBots}
	routes.SetupRoutes(api, &environment, config.AdminAPIKey)
	if config.AdminAPIKey == "" {
		logger.Warn("POST /map/create refuses every request because " + dependencies.AdminAPIKeyEnv + " is not set")
	}
	if selected.gameGraph {
		routes.SetupGameGraphRoute(api, &environment)
	}
	if lobby != nil {
		// The lobby's player API, game API, Centrifugo proxy and page, which
		// / opens.
		if err := lobby.Register(router, api); err != nil {
			return nil, err
		}
		router.Get("/", func(response http.ResponseWriter, request *http.Request) {
			http.Redirect(response, request, lobbyapp.UIPath, http.StatusTemporaryRedirect)
		})
	}
	routes.SetupGameRoutes(api, &handlers.LobbyGame{Database: database, Players: handlers.PlayerTokens{Verifier: playerVerifier}, Publisher: gamePublisher, Retention: gameRetention, Watchers: lobbyClient,
		SubmitTokens: submitTokens, PublicURL: config.PublicURL, Bots: bots})
	routes.SetupBotPackageRoutes(api, &handlers.BotPackages{Players: handlers.PlayerTokens{Verifier: playerVerifier}, Service: services.BotPackages{
		Database: database, Lobbies: lobbyClient, Store: packages, Tokens: uploadTokens}})
	// The game's hooks, which the lobby signs its calls of.
	router.Handle(lobbyadapter.HooksPath+"/*", hooks)
	if config.BotPackagesBucket == "" && selected.localPackagesHandler != nil {
		router.Handle(selected.localPackagesPrefix+"/*", selected.localPackagesHandler)
	}
	if err := registerPages(router); err != nil {
		return nil, err
	}
	router.Get("/healthz", healthz(Version))
	return router, nil
}

// inProcessLobby builds the lobby in this process with lobby/app, from the
// backend's settings, as the lobby's own hosts build it from LOBBY_*. Its
// LOBBY_GAMES lists Island Battles, whose hooks it calls through loopback,
// which also carries the game's calls of its game API to it. The lobby's
// authenticated players, and the causes that its answers leave out, go on
// the request's error line.
func inProcessLobby(config dependencies.Config, database *gorm.DB, centrifugo *gocent.Client, logger *slog.Logger, loopback *Loopback, hooks func() http.Handler) (*lobbyapp.Lobby, error) {
	games, err := config.LobbyGamesConfig()
	if err != nil {
		return nil, err
	}
	hooksURL, err := islandBattlesHooksURL(games)
	if err != nil {
		return nil, err
	}
	loopback.Handle(hooksURL, hooks)
	lobby, err := lobbyapp.New(lobbyapp.Config{
		PublicURL:             config.LobbyIssuer(),
		CentrifugoAPIURL:      config.CentrifugoAPIURL,
		CentrifugoAPIKey:      config.CentrifugoAPIKey,
		CentrifugoPublicURL:   config.CentrifugoPublicURL,
		ConnectionTokenSecret: config.ConnectionTokenHMACSecret,
		ProxySecret:           config.LobbyProxySecret,
		PlayerTokenKeys:       config.PlayerTokenKeys,
		PlayerTokenLifetime:   config.PlayerTokenLifetime,
		Games:                 games,
		GameSecrets:           config.LobbyGameSecrets,
	}, database, centrifugo, logger,
		lobbyapp.WithHookOptions(gamehooks.WithHTTPClient(loopback.Client())),
		lobbyapp.WithObserver(lobbyapp.Observer{
			Authenticated: func(ctx context.Context, playerID string) {
				observability.Annotate(ctx, slog.String("player_id", playerID))
			},
			Error: observability.RecordError,
		}))
	if err != nil {
		return nil, err
	}
	loopback.Handle(LoopbackOrigin+gamehooks.GameAPIPath, lobby.GameAPI)
	return lobby, nil
}

// turnSender sends hosted and uploaded bots' turns (#225) while the
// configuration offers them: to the runner functions it names, or else to
// the local server host's own runners. A host with neither, such as the
// Lambda host without the functions' names, sends nothing, and those bots
// lose at their first deadline.
func turnSender(config dependencies.Config, selected options, packages packagestore.Store, tokens *submittoken.Signer, logger *slog.Logger) (*services.TurnSender, error) {
	if !config.ServerBots {
		return nil, nil
	}
	origin := strings.TrimRight(config.PublicURL, "/")
	if origin == "" {
		origin = strings.TrimRight(selected.localOrigin, "/")
	}
	sender := &services.TurnSender{Tokens: tokens}
	if packages != nil {
		sender.Packages = packages
	}
	if origin != "" {
		sender.SubmitURL = origin + "/generation/commands"
	} else {
		logger.Warn("Hosted and uploaded bots get no turns because " + dependencies.PublicURLEnv + " is not set")
	}
	switch {
	case config.BotRunnerPython != "":
		runners, err := botrunner.LambdaFromEnvironment(context.Background(), config.BotRunnerPython, config.BotRunnerNative)
		if err != nil {
			return nil, err
		}
		sender.Runners = runners
	case selected.localRunners != nil:
		sender.Runners = selected.localRunners
	default:
		logger.Warn("Hosted and uploaded bots get no turns because " + dependencies.BotRunnerPythonEnv + " and " + dependencies.BotRunnerNativeEnv + " are not set")
	}
	return sender, nil
}

// registerPages serves the game's embedded browser pages, which need no files
// beside the binary: the lobby page's game modules, the map builder and the
// demo viewer.
func registerPages(router chi.Router) error {
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

// publications publishes the game's events with the lobby's publisher.
type publications struct{ *realtime.Publisher }

func (publisher publications) Publish(ctx context.Context, events ...services.Publication) error {
	converted := make([]realtime.Publication, len(events))
	for index, event := range events {
		converted[index] = realtime.Publication{Channel: event.Channel, Data: event.Data}
	}
	return publisher.Publisher.Publish(ctx, converted...)
}

// islandBattlesHooksURL returns the hooks_url of island-battles in a
// LOBBY_GAMES value, whose calls the loopback serves in this process.
func islandBattlesHooksURL(lobbyGames string) (string, error) {
	var entries []struct {
		Key      string `json:"key"`
		HooksURL string `json:"hooks_url"`
	}
	if err := json.Unmarshal([]byte(lobbyGames), &entries); err != nil {
		return "", err
	}
	for _, entry := range entries {
		if entry.Key == lobbyadapter.GameKey {
			parsed, err := gamehooks.ParseServiceURL(entry.HooksURL)
			if err != nil {
				return "", err
			}
			return parsed.String(), nil
		}
	}
	return "", fmt.Errorf("%s lists no %s", gamehooks.GamesEnv, lobbyadapter.GameKey)
}
