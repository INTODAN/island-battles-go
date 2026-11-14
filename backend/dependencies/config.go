package dependencies

import (
	"fmt"
	"math"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/INTODAN/mpl/gamehooks"
	"islandBattles/backend/retention"
	"islandBattles/backend/services"
)

const (
	DatabaseURLEnv                   = "ISLAND_BATTLES_DATABASE_URL"
	DatabaseMaxOpenConnectionsEnv    = "ISLAND_BATTLES_DATABASE_MAX_OPEN_CONNECTIONS"
	DatabaseMaxIdleConnectionsEnv    = "ISLAND_BATTLES_DATABASE_MAX_IDLE_CONNECTIONS"
	DatabaseConnectionMaxIdleTimeEnv = "ISLAND_BATTLES_DATABASE_CONNECTION_MAX_IDLE_TIME"
	HTTPAddrEnv                      = "ISLAND_BATTLES_HTTP_ADDR"
	CentrifugoAPIURLEnv              = "ISLAND_BATTLES_CENTRIFUGO_API_URL"
	CentrifugoAPIKeyEnv              = "ISLAND_BATTLES_CENTRIFUGO_API_KEY"
	SubmitTokenSecretEnv             = "ISLAND_BATTLES_SUBMIT_TOKEN_SECRET"
	PublicURLEnv                     = "ISLAND_BATTLES_PUBLIC_URL"
	AdminAPIKeyEnv                   = "ISLAND_BATTLES_ADMIN_API_KEY"
	GameRetentionPeriodEnv           = "ISLAND_BATTLES_GAME_RETENTION_PERIOD"
	BotPackagesBucketEnv             = "ISLAND_BATTLES_BOT_PACKAGES_BUCKET"
	ServerBotsEnv                    = "ISLAND_BATTLES_SERVER_BOTS"
	BotRunnerPythonEnv               = "ISLAND_BATTLES_BOT_RUNNER_PYTHON"
	BotRunnerNativeEnv               = "ISLAND_BATTLES_BOT_RUNNER_NATIVE"
	BotComputeBudgetEnv              = "ISLAND_BATTLES_BOT_COMPUTE_BUDGET_GB_SECONDS"
	// BotComputeFakeEnv sets the bot runners' compute this month, in place
	// of CloudWatch, for local development and tests; see
	// Config.BotComputeFakeGBSeconds.
	BotComputeFakeEnv = "ISLAND_BATTLES_BOT_COMPUTE_FAKE_GB_SECONDS"
	// LocalBotPythonEnv is read by the local server host alone; see
	// LoadLocalBotPython.
	LocalBotPythonEnv = "ISLAND_BATTLES_LOCAL_BOT_PYTHON"
	// LobbyHookSecretEnv is the secret that signs the lobby's calls of the
	// game's hooks and the game's calls of the lobby's game API: the lobby's
	// LOBBY_GAME_SECRET_ISLAND_BATTLES, by the game's name for it.
	LobbyHookSecretEnv = "ISLAND_BATTLES_LOBBY_HOOK_SECRET"
	// LobbyURLEnv is the lobby's URL, such as https://lobby.example.org: the
	// lobby runs on its own (github.com/INTODAN/mpl), and the game reads its
	// game API and its JWKS there. Required.
	LobbyURLEnv = "ISLAND_BATTLES_LOBBY_URL"
	// LobbyOriginsEnv lists the lobby's sites whose lobby page loads the
	// game's modules and calls its API from another origin; see
	// Config.LobbyOrigins.
	LobbyOriginsEnv = "ISLAND_BATTLES_LOBBY_ORIGINS"
	// BotPackagesDirEnv is read by the local server host alone; see
	// LoadBotPackagesDir.
	BotPackagesDirEnv = "ISLAND_BATTLES_BOT_PACKAGES_DIR"
)

// The pool defaults suit Lambda, where an instance serves one request at a
// time and every instance has its own pool.
const (
	DefaultDatabaseMaxOpenConnections    = 2
	DefaultDatabaseMaxIdleConnections    = 2
	DefaultDatabaseConnectionMaxIdleTime = time.Minute
)

// DatabaseConfig says where PostgreSQL is and bounds this process's pool.
type DatabaseConfig struct {
	URL                   string
	MaxOpenConnections    int
	MaxIdleConnections    int
	ConnectionMaxIdleTime time.Duration
}

// DefaultDatabaseConfig connects to url with the default pool limits.
func DefaultDatabaseConfig(url string) DatabaseConfig {
	return DatabaseConfig{
		URL:                   url,
		MaxOpenConnections:    DefaultDatabaseMaxOpenConnections,
		MaxIdleConnections:    DefaultDatabaseMaxIdleConnections,
		ConnectionMaxIdleTime: DefaultDatabaseConnectionMaxIdleTime,
	}
}

// Config contains the settings every host of the HTTP API needs. They must be
// supplied explicitly by the deployment, except for the pool limits and the
// game retention period, which have defaults, the admin API key, without
// which admin operations refuse every request, and the public URL, which
// defaults to each request's own. The lobby runs on its own, at LobbyURL,
// with its own settings. Values are loaded only by executables, never by
// package init.
// The listen address belongs to the HTTP server host alone; see LoadHTTPAddr.
type Config struct {
	Database DatabaseConfig
	// CentrifugoAPIURL and CentrifugoAPIKey are Centrifugo's HTTP API, which
	// the game publishes its events with.
	CentrifugoAPIURL string
	CentrifugoAPIKey string
	// SubmitTokenSecret signs the submit tokens of turn deliveries.
	SubmitTokenSecret string
	// PublicURL is where players open the game, such as
	// https://island-battles.example.org. Turn deliveries tell bots to submit
	// to it. Empty means the scheme and Host of each request, which suits a
	// server that players reach directly, such as a local one.
	PublicURL string
	// AdminAPIKey authorizes admin operations such as POST /map/create. Empty
	// disables them.
	AdminAPIKey string
	// GameRetentionPeriod is how long a game is kept after its latest
	// generation was created; see the retention package.
	GameRetentionPeriod time.Duration
	// BotPackagesBucket is the S3 bucket that keeps uploaded bot packages,
	// in the region and with the credentials of the environment. Empty
	// means none: the local server host then keeps them on disk, and the
	// Lambda host refuses uploads.
	BotPackagesBucket string
	// ServerBots offers the bots that the server plays without a player's
	// tab, hosted and uploaded, as a player's own bot and for bot seats, and
	// sends them their turns: ISLAND_BATTLES_SERVER_BOTS=on, the default.
	// Off, players see the localhost and default bots only, and no turn is
	// sent.
	ServerBots bool
	// BotRunnerPython and BotRunnerNative name the bot runner functions
	// that play hosted and uploaded bots' turns, invoked in the region and
	// with the credentials of the environment. Both or neither are set.
	// Neither means none: the local server host then runs the runners in
	// its own process, and the Lambda host sends no turns.
	BotRunnerPython string
	BotRunnerNative string
	// BotComputeBudgetGBSeconds is the bot runners' monthly compute budget
	// (#226): once they have used it, no game with an uploaded bot starts
	// until the next month (UTC). 300,000 by default.
	BotComputeBudgetGBSeconds float64
	// BotComputeFakeGBSeconds, when set, is the runners' compute this month,
	// for local development and tests. The runners' compute is read from
	// CloudWatch while the runner functions are named; otherwise, as on a
	// local server, the guard is off unless this is set. It can't be set
	// with the runner functions.
	BotComputeFakeGBSeconds *float64
	// LobbyHookSecret signs the lobby's calls of Island Battles' hooks and
	// Island Battles' calls of the lobby's game API, at least
	// gamehooks.MinSecretBytes bytes.
	LobbyHookSecret []byte
	// LobbyURL is the lobby's URL, such as https://lobby.example.org or
	// http://localhost:8890: the game reads its game API and its JWKS there,
	// and it is the iss of the lobby's player tokens, so the lobby's
	// LOBBY_PUBLIC_URL must be the same.
	LobbyURL string
	// LobbyOrigins are the lobby's sites, such as https://lobby.example.org,
	// whose lobby page loads Island Battles' modules and documents and calls
	// its API from another origin: the game answers them with CORS, and the
	// map builder hands its map to a lobby page there. Empty, no other site
	// may.
	LobbyOrigins []string
}

// validateLobby checks the binding to the lobby: the hook secret's length,
// and that the lobby's URL is an origin.
func (config Config) validateLobby() error {
	if len(config.LobbyHookSecret) < gamehooks.MinSecretBytes {
		return fmt.Errorf("%s must hold a secret of at least %d bytes", LobbyHookSecretEnv, gamehooks.MinSecretBytes)
	}
	parsed, err := gamehooks.ParseServiceURL(config.LobbyURL)
	if err != nil {
		return fmt.Errorf("%s: %w", LobbyURLEnv, err)
	}
	if parsed.Path != "" {
		return fmt.Errorf("%s must be an origin, such as https://lobby.example.org, without a path", LobbyURLEnv)
	}
	return nil
}

// LoadConfig reads and validates the HTTP API's environment values.
func LoadConfig() (Config, error) {
	return LoadConfigFromLookup(os.LookupEnv)
}

// LoadConfigFromLookup makes configuration validation deterministic in tests.
func LoadConfigFromLookup(lookup func(string) (string, bool)) (Config, error) {
	if err := requireEnvironment(lookup, DatabaseURLEnv, CentrifugoAPIURLEnv, CentrifugoAPIKeyEnv, SubmitTokenSecretEnv, LobbyHookSecretEnv, LobbyURLEnv); err != nil {
		return Config{}, err
	}
	database, err := LoadDatabaseConfigFromLookup(lookup)
	if err != nil {
		return Config{}, err
	}
	gameRetentionPeriod, err := LoadGameRetentionPeriodFromLookup(lookup)
	if err != nil {
		return Config{}, err
	}
	serverBots := true
	switch readEnvironment(lookup, ServerBotsEnv) {
	case "", "on":
	case "off":
		serverBots = false
	default:
		return Config{}, fmt.Errorf("%s must be on or off", ServerBotsEnv)
	}
	botRunnerPython, botRunnerNative := readEnvironment(lookup, BotRunnerPythonEnv), readEnvironment(lookup, BotRunnerNativeEnv)
	if (botRunnerPython == "") != (botRunnerNative == "") {
		return Config{}, fmt.Errorf("set both %s and %s, or neither", BotRunnerPythonEnv, BotRunnerNativeEnv)
	}
	lobbyOrigins, err := parseOrigins(readEnvironment(lookup, LobbyOriginsEnv))
	if err != nil {
		return Config{}, fmt.Errorf("%s: %w", LobbyOriginsEnv, err)
	}
	budget, err := readGBSeconds(lookup, BotComputeBudgetEnv)
	if err != nil {
		return Config{}, err
	}
	if budget == nil {
		defaultBudget := float64(services.DefaultBotComputeBudgetGBSeconds)
		budget = &defaultBudget
	}
	fakeCompute, err := readGBSeconds(lookup, BotComputeFakeEnv)
	if err != nil {
		return Config{}, err
	}
	if fakeCompute != nil && botRunnerPython != "" {
		return Config{}, fmt.Errorf("%s fakes the bot runners' compute where they run locally; unset it while %s and %s name the runner functions, whose compute is read from CloudWatch", BotComputeFakeEnv, BotRunnerPythonEnv, BotRunnerNativeEnv)
	}
	config := Config{
		Database:                  database,
		CentrifugoAPIURL:          readEnvironment(lookup, CentrifugoAPIURLEnv),
		CentrifugoAPIKey:          readEnvironment(lookup, CentrifugoAPIKeyEnv),
		SubmitTokenSecret:         readEnvironment(lookup, SubmitTokenSecretEnv),
		PublicURL:                 strings.TrimRight(readEnvironment(lookup, PublicURLEnv), "/"),
		AdminAPIKey:               readEnvironment(lookup, AdminAPIKeyEnv),
		GameRetentionPeriod:       gameRetentionPeriod,
		BotPackagesBucket:         readEnvironment(lookup, BotPackagesBucketEnv),
		ServerBots:                serverBots,
		BotRunnerPython:           botRunnerPython,
		BotRunnerNative:           botRunnerNative,
		BotComputeBudgetGBSeconds: *budget,
		BotComputeFakeGBSeconds:   fakeCompute,
		LobbyHookSecret:           []byte(readEnvironment(lookup, LobbyHookSecretEnv)),
		LobbyURL:                  strings.TrimRight(readEnvironment(lookup, LobbyURLEnv), "/"),
		LobbyOrigins:              lobbyOrigins,
	}
	if err := validateURL(CentrifugoAPIURLEnv, config.CentrifugoAPIURL, "http", "https"); err != nil {
		return Config{}, err
	}
	if config.PublicURL != "" {
		if err := validateURL(PublicURLEnv, config.PublicURL, "http", "https"); err != nil {
			return Config{}, err
		}
		if parsed, _ := url.Parse(config.PublicURL); parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
			return Config{}, fmt.Errorf("%s must be an origin, such as https://island-battles.example.org, without a path", PublicURLEnv)
		}
	}
	if err := config.validateLobby(); err != nil {
		return Config{}, err
	}
	return config, nil
}

// LoadGameRetentionPeriodFromLookup reads ISLAND_BATTLES_GAME_RETENTION_PERIOD,
// or returns retention.DefaultPeriod when it isn't set. The admin MCP server
// reads it too, to say when retention deletes a game.
func LoadGameRetentionPeriodFromLookup(lookup func(string) (string, bool)) (time.Duration, error) {
	value := readEnvironment(lookup, GameRetentionPeriodEnv)
	if value == "" {
		return retention.DefaultPeriod, nil
	}
	period, err := time.ParseDuration(value)
	if err != nil || period <= 0 {
		return 0, fmt.Errorf("%s must be a positive duration such as 168h", GameRetentionPeriodEnv)
	}
	return period, nil
}

// LoadDatabaseConfig reads the database settings, which are all the migrate
// command needs.
func LoadDatabaseConfig() (DatabaseConfig, error) {
	return LoadDatabaseConfigFromLookup(os.LookupEnv)
}

// LoadDatabaseConfigFromLookup requires the database URL and applies the
// default pool limits where none are set.
func LoadDatabaseConfigFromLookup(lookup func(string) (string, bool)) (DatabaseConfig, error) {
	if err := requireEnvironment(lookup, DatabaseURLEnv); err != nil {
		return DatabaseConfig{}, err
	}
	config := DefaultDatabaseConfig(readEnvironment(lookup, DatabaseURLEnv))
	if err := validateURL(DatabaseURLEnv, config.URL, "postgres", "postgresql"); err != nil {
		return DatabaseConfig{}, err
	}
	var err error
	if config.MaxOpenConnections, err = readCount(lookup, DatabaseMaxOpenConnectionsEnv, config.MaxOpenConnections, 1); err != nil {
		return DatabaseConfig{}, err
	}
	if config.MaxIdleConnections, err = readCount(lookup, DatabaseMaxIdleConnectionsEnv, min(config.MaxIdleConnections, config.MaxOpenConnections), 0); err != nil {
		return DatabaseConfig{}, err
	}
	if config.MaxIdleConnections > config.MaxOpenConnections {
		return DatabaseConfig{}, fmt.Errorf("%s must not exceed %s", DatabaseMaxIdleConnectionsEnv, DatabaseMaxOpenConnectionsEnv)
	}
	if value := readEnvironment(lookup, DatabaseConnectionMaxIdleTimeEnv); value != "" {
		config.ConnectionMaxIdleTime, err = time.ParseDuration(value)
		if err != nil || config.ConnectionMaxIdleTime <= 0 {
			return DatabaseConfig{}, fmt.Errorf("%s must be a positive duration such as 1m", DatabaseConnectionMaxIdleTimeEnv)
		}
	}
	return config, nil
}

// LoadBotPackagesDir reads the folder where the local server host keeps bot
// packages when no bucket is configured, island-battles-bot-packages in the
// system's temporary folder by default.
func LoadBotPackagesDir(lookup func(string) (string, bool)) string {
	if directory := readEnvironment(lookup, BotPackagesDirEnv); directory != "" {
		return directory
	}
	return filepath.Join(os.TempDir(), "island-battles-bot-packages")
}

// LoadLocalBotPython reads the command that runs Python 3.14 for the local
// server host's Python bot runner, which runs uploaded Python bots when no
// runner functions are configured: python3.14 by default, or
// ISLAND_BATTLES_LOCAL_BOT_PYTHON split at spaces, such as
// "uv run --no-project --python 3.14 python".
func LoadLocalBotPython(lookup func(string) (string, bool)) []string {
	if command := strings.Fields(readEnvironment(lookup, LocalBotPythonEnv)); len(command) > 0 {
		return command
	}
	return []string{"python3.14"}
}

// LoadHTTPAddr reads the address the HTTP server host listens on.
func LoadHTTPAddr() (string, error) {
	return LoadHTTPAddrFromLookup(os.LookupEnv)
}

// LoadHTTPAddrFromLookup makes address validation deterministic in tests.
func LoadHTTPAddrFromLookup(lookup func(string) (string, bool)) (string, error) {
	if err := requireEnvironment(lookup, HTTPAddrEnv); err != nil {
		return "", err
	}
	address := readEnvironment(lookup, HTTPAddrEnv)
	if _, _, err := net.SplitHostPort(address); err != nil {
		return "", fmt.Errorf("%s must be a host:port address: %w", HTTPAddrEnv, err)
	}
	return address, nil
}

func readEnvironment(lookup func(string) (string, bool), name string) string {
	value, ok := lookup(name)
	if !ok {
		return ""
	}
	return strings.TrimSpace(value)
}

// requireEnvironment names every missing variable at once, without values.
func requireEnvironment(lookup func(string) (string, bool), names ...string) error {
	missing := make([]string, 0)
	for _, name := range names {
		if readEnvironment(lookup, name) == "" {
			missing = append(missing, name)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	sort.Strings(missing)
	return fmt.Errorf("missing required environment variables: %s", strings.Join(missing, ", "))
}

// parseOrigins reads a list of origins separated by commas, such as
// "https://lobby.example.org, http://localhost:8890": each an http or https
// origin as browsers send it in the Origin header, in lower case, without a
// path, a trailing slash or a default port. Empty, it lists none.
func parseOrigins(value string) ([]string, error) {
	var origins []string
	for item := range strings.SplitSeq(value, ",") {
		origin := strings.TrimSpace(item)
		if origin == "" {
			continue
		}
		parsed, err := url.Parse(origin)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" ||
			origin != strings.ToLower(parsed.Scheme+"://"+parsed.Host) ||
			(parsed.Scheme == "https" && parsed.Port() == "443") || (parsed.Scheme == "http" && parsed.Port() == "80") {
			return nil, fmt.Errorf("%q is not an origin such as https://lobby.example.org, without a path", origin)
		}
		origins = append(origins, origin)
	}
	return origins, nil
}

// readGBSeconds reads an optional number of GB-seconds, nil when unset.
func readGBSeconds(lookup func(string) (string, bool), name string) (*float64, error) {
	value := readEnvironment(lookup, name)
	if value == "" {
		return nil, nil
	}
	amount, err := strconv.ParseFloat(value, 64)
	if err != nil || amount < 0 || math.IsInf(amount, 0) || math.IsNaN(amount) {
		return nil, fmt.Errorf("%s must be a number of GB-seconds, 0 or more, such as 300000", name)
	}
	return &amount, nil
}

// readCount reads an optional whole number of at least minimum.
func readCount(lookup func(string) (string, bool), name string, fallback int, minimum int) (int, error) {
	value := readEnvironment(lookup, name)
	if value == "" {
		return fallback, nil
	}
	count, err := strconv.Atoi(value)
	if err != nil || count < minimum {
		return 0, fmt.Errorf("%s must be a whole number of at least %d", name, minimum)
	}
	return count, nil
}

func validateURL(environmentName string, value string, allowedSchemes ...string) error {
	parsedURL, err := url.ParseRequestURI(value)
	if err != nil || parsedURL.Host == "" {
		return fmt.Errorf("%s must be an absolute URL", environmentName)
	}
	for _, scheme := range allowedSchemes {
		if parsedURL.Scheme == scheme {
			return nil
		}
	}
	return fmt.Errorf("%s has unsupported URL scheme %q", environmentName, parsedURL.Scheme)
}
