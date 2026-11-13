package dependencies

import (
	"encoding/json"
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
	"github.com/INTODAN/mpl/playertoken"
	"islandBattles/backend/lobbyadapter"
	"islandBattles/backend/retention"
	"islandBattles/backend/services"
)

const (
	DatabaseURLEnv                   = "ISLAND_BATTLES_DATABASE_URL"
	DatabaseMaxOpenConnectionsEnv    = "ISLAND_BATTLES_DATABASE_MAX_OPEN_CONNECTIONS"
	DatabaseMaxIdleConnectionsEnv    = "ISLAND_BATTLES_DATABASE_MAX_IDLE_CONNECTIONS"
	DatabaseConnectionMaxIdleTimeEnv = "ISLAND_BATTLES_DATABASE_CONNECTION_MAX_IDLE_TIME"
	HTTPAddrEnv                      = "ISLAND_BATTLES_HTTP_ADDR"
	CentrifugoPublicURLEnv           = "ISLAND_BATTLES_CENTRIFUGO_PUBLIC_URL"
	CentrifugoAPIURLEnv              = "ISLAND_BATTLES_CENTRIFUGO_API_URL"
	CentrifugoAPIKeyEnv              = "ISLAND_BATTLES_CENTRIFUGO_API_KEY"
	ConnectionTokenHMACSecretEnv     = "ISLAND_BATTLES_CONNECTION_TOKEN_HMAC_SECRET"
	LobbyProxySecretEnv              = "ISLAND_BATTLES_LOBBY_PROXY_SECRET"
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
	// LobbyURLEnv is the URL of a lobby that runs on its own (github.com/INTODAN/mpl/cmd/lobby),
	// whose game API and JWKS the game reads. Empty, the lobby runs in this
	// process and the two call each other in memory.
	LobbyURLEnv = "ISLAND_BATTLES_LOBBY_URL"
	// LobbyOriginsEnv lists the lobby's sites whose lobby page loads the
	// game's modules and calls its API from another origin; see
	// Config.LobbyOrigins.
	LobbyOriginsEnv = "ISLAND_BATTLES_LOBBY_ORIGINS"
	// BotPackagesDirEnv is read by the local server host alone; see
	// LoadBotPackagesDir.
	BotPackagesDirEnv = "ISLAND_BATTLES_BOT_PACKAGES_DIR"
	// The lobby's settings keep the lobby's own prefix, since the lobby
	// becomes its own service (#118).
	PlayerTokenKeysEnv     = "LOBBY_PLAYER_TOKEN_KEYS"
	PlayerTokenLifetimeEnv = "LOBBY_PLAYER_TOKEN_LIFETIME"
	// LobbyDatabaseURLEnv is the lobby's own database (#229), on the same
	// server as Island Battles' or another: the lobby in this process keeps
	// its tables there, and the migrate command drops the lobby's tables from
	// Island Battles' database once it names another database.
	LobbyDatabaseURLEnv = "LOBBY_DATABASE_URL"
	// LobbyGamesEnv lists the games that the lobby calls over HTTP; see
	// Config.LobbyGames.
	LobbyGamesEnv = gamehooks.GamesEnv
)

// LocalLobbyIssuer is the iss of player tokens when ISLAND_BATTLES_PUBLIC_URL
// is unset, as on a local server. The lobby and the game share the process
// and its settings, so they agree on it.
const LocalLobbyIssuer = "urn:island-battles:local-lobby"

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
// defaults to each request's own. With LobbyURL set, the lobby runs on its
// own, and the settings that only the lobby reads are neither required nor
// read: the lobby's database, the Centrifugo public URL, the connection token and proxy secrets,
// the player token keys and lifetime, and LOBBY_GAMES. Values are loaded
// only by executables, never by package init.
// The listen address belongs to the HTTP server host alone; see LoadHTTPAddr.
type Config struct {
	Database                  DatabaseConfig
	CentrifugoPublicURL       string
	CentrifugoAPIURL          string
	CentrifugoAPIKey          string
	ConnectionTokenHMACSecret string
	LobbyProxySecret          string
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
	// PlayerTokenKeys are the lobby's player token keys, kid:seed pairs; the
	// first signs. See playertoken.ParseKeySet.
	PlayerTokenKeys *playertoken.KeySet
	// PlayerTokenLifetime is how long a player token lasts:
	// playertoken.MaxLifetime unless a development setup shortens it.
	PlayerTokenLifetime time.Duration
	// LobbyHookSecret signs the lobby's calls of Island Battles' hooks and
	// Island Battles' calls of the lobby's game API, at least
	// gamehooks.MinSecretBytes bytes.
	LobbyHookSecret []byte
	// LobbyURL is the URL of a lobby that runs on its own, such as
	// http://localhost:8890: the game reads its game API and its JWKS there,
	// and it is the iss of the lobby's player tokens, so the lobby's
	// LOBBY_PUBLIC_URL must be the same. Empty, the lobby runs in this
	// process, and the lobby's hook calls and the game's game API calls reach
	// the other side's handler in memory.
	LobbyURL string
	// LobbyDatabaseURL is the lobby's own database, LOBBY_DATABASE_URL,
	// which the lobby in this process keeps its tables in. Required while
	// the lobby runs in this process, and not read otherwise.
	LobbyDatabaseURL string
	// LobbyGames is the lobby's LOBBY_GAMES: the games it calls over HTTP,
	// as a JSON array of gamehooks.GameConfig. Island Battles is added, as
	// lobbyadapter.LobbyGame, unless it lists island-battles itself; see
	// LobbyGamesConfig.
	LobbyGames string
	// LobbyGameSecrets are the values of the variables that the lobby's games
	// name as their secret_env, by name.
	LobbyGameSecrets map[string]string
	// LobbyOrigins are the lobby's sites, such as https://lobby.example.org,
	// whose lobby page loads Island Battles' modules and documents and calls
	// its API from another origin: the game answers them with CORS, and the
	// map builder hands its map to a lobby page there. Empty, as while the
	// lobby runs in this process and its page is on the game's own origin,
	// no other site may.
	LobbyOrigins []string
}

// LobbyGamesConfig returns the lobby's LOBBY_GAMES with Island Battles in
// it: LobbyGames, plus lobbyadapter.LobbyGame unless LobbyGames lists
// island-battles itself. That entry's hooks_url is the public URL's
// lobbyadapter.HooksPath, or http://localhost's without a public URL, which
// suits a lobby in this process: its hook calls never leave it.
//
// frontendOrigin is where browsers reach the game, for a lobby page on
// another origin: the entry's catalog then has absolute URLs there, with
// frontendOrigin as its origin. Empty, as for the lobby in this process,
// whose page is on the game's origin, the catalog has paths.
func (config Config) LobbyGamesConfig(frontendOrigin string) (string, error) {
	entries := []json.RawMessage{}
	if strings.TrimSpace(config.LobbyGames) != "" {
		if err := json.Unmarshal([]byte(config.LobbyGames), &entries); err != nil {
			return "", fmt.Errorf("%s must be a JSON array: %w", LobbyGamesEnv, err)
		}
	}
	for _, entry := range entries {
		var game struct {
			Key string `json:"key"`
		}
		if err := json.Unmarshal(entry, &game); err == nil && game.Key == lobbyadapter.GameKey {
			return config.LobbyGames, nil
		}
	}
	origin := config.PublicURL
	if origin == "" {
		origin = "http://localhost"
	}
	islandBattles, err := lobbyadapter.LobbyGame(origin+lobbyadapter.HooksPath, frontendOrigin)
	if err != nil {
		return "", err
	}
	games, err := json.Marshal(append(entries, islandBattles))
	return string(games), err
}

// LookupLobbyGameSecret reads a game's shared secret by the variable that
// its secret_env names.
func (config Config) LookupLobbyGameSecret(name string) (string, bool) {
	secret, found := config.LobbyGameSecrets[name]
	return secret, found
}

// loadLobbyGames reads the secrets of the lobby's games and checks the
// lobby's binding to Island Battles: LOBBY_GAMES must parse, with Island
// Battles added, and ISLAND_BATTLES_LOBBY_HOOK_SECRET must equal the lobby's
// secret for Island Battles while the lobby runs in this process. A lobby
// that runs on its own reads its games itself.
func (config *Config) loadLobbyGames(lookup func(string) (string, bool)) error {
	if len(config.LobbyHookSecret) < gamehooks.MinSecretBytes {
		return fmt.Errorf("%s must hold a secret of at least %d bytes", LobbyHookSecretEnv, gamehooks.MinSecretBytes)
	}
	if config.LobbyURL != "" {
		parsed, err := gamehooks.ParseServiceURL(config.LobbyURL)
		if err != nil {
			return fmt.Errorf("%s: %w", LobbyURLEnv, err)
		}
		if parsed.Path != "" {
			return fmt.Errorf("%s must be an origin, such as https://lobby.example.org, without a path", LobbyURLEnv)
		}
		return nil
	}
	var listed []struct {
		SecretEnv string `json:"secret_env"`
	}
	if strings.TrimSpace(config.LobbyGames) != "" {
		if err := json.Unmarshal([]byte(config.LobbyGames), &listed); err != nil {
			return fmt.Errorf("%s must be a JSON array: %w", LobbyGamesEnv, err)
		}
	}
	names := []string{lobbyadapter.SecretEnv}
	for _, game := range listed {
		names = append(names, game.SecretEnv)
	}
	config.LobbyGameSecrets = map[string]string{}
	for _, name := range names {
		if secret, found := lookup(name); found && name != "" {
			config.LobbyGameSecrets[name] = secret
		}
	}
	games, err := config.LobbyGamesConfig("")
	if err != nil {
		return err
	}
	configured, err := gamehooks.ParseGames(games, config.LookupLobbyGameSecret)
	if err != nil {
		return err
	}
	for _, game := range configured {
		if game.Registration.Key == lobbyadapter.GameKey && config.LobbyURL == "" && string(game.Secret) != string(config.LobbyHookSecret) {
			return fmt.Errorf("%s must equal the lobby's secret for island-battles while the lobby runs in this process", LobbyHookSecretEnv)
		}
	}
	return nil
}

// LobbyDatabaseConfig is the lobby's own database, for the lobby in this
// process, with the pool limits of Island Battles' database: each host keeps
// one pool of each.
func (config Config) LobbyDatabaseConfig() DatabaseConfig {
	lobby := config.Database
	lobby.URL = config.LobbyDatabaseURL
	return lobby
}

// LobbyIssuer is the iss of the lobby's player tokens: the URL of a lobby
// that runs on its own; or, while the lobby runs in this process, the public
// URL, which the two share, or LocalLobbyIssuer without one.
func (config Config) LobbyIssuer() string {
	if config.LobbyURL != "" {
		return config.LobbyURL
	}
	if config.PublicURL == "" {
		return LocalLobbyIssuer
	}
	return config.PublicURL
}

// LoadConfig reads and validates the HTTP API's environment values.
func LoadConfig() (Config, error) {
	return LoadConfigFromLookup(os.LookupEnv)
}

// LoadConfigFromLookup makes configuration validation deterministic in tests.
func LoadConfigFromLookup(lookup func(string) (string, bool)) (Config, error) {
	lobbyURL := strings.TrimRight(readEnvironment(lookup, LobbyURLEnv), "/")
	required := []string{DatabaseURLEnv, CentrifugoAPIURLEnv, CentrifugoAPIKeyEnv, SubmitTokenSecretEnv, LobbyHookSecretEnv}
	if lobbyURL == "" {
		// The lobby in this process reads these too.
		required = append(required, LobbyDatabaseURLEnv, CentrifugoPublicURLEnv, ConnectionTokenHMACSecretEnv, LobbyProxySecretEnv, PlayerTokenKeysEnv)
	}
	if err := requireEnvironment(lookup, required...); err != nil {
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
	var playerTokenKeys *playertoken.KeySet
	playerTokenLifetime := playertoken.MaxLifetime
	if lobbyURL == "" {
		playerTokenKeys, err = playertoken.ParseKeySet(readEnvironment(lookup, PlayerTokenKeysEnv))
		if err != nil {
			// The error names no seed.
			return Config{}, fmt.Errorf("%s: %w", PlayerTokenKeysEnv, err)
		}
		if value := readEnvironment(lookup, PlayerTokenLifetimeEnv); value != "" {
			playerTokenLifetime, err = time.ParseDuration(value)
			if err != nil || playerTokenLifetime < time.Minute || playerTokenLifetime > playertoken.MaxLifetime {
				return Config{}, fmt.Errorf("%s must be a duration from 1m to 15m", PlayerTokenLifetimeEnv)
			}
		}
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
		PlayerTokenKeys:           playerTokenKeys,
		PlayerTokenLifetime:       playerTokenLifetime,
		LobbyHookSecret:           []byte(readEnvironment(lookup, LobbyHookSecretEnv)),
		LobbyURL:                  lobbyURL,
		LobbyOrigins:              lobbyOrigins,
	}
	if lobbyURL == "" {
		config.CentrifugoPublicURL = readEnvironment(lookup, CentrifugoPublicURLEnv)
		config.ConnectionTokenHMACSecret = readEnvironment(lookup, ConnectionTokenHMACSecretEnv)
		config.LobbyProxySecret = readEnvironment(lookup, LobbyProxySecretEnv)
		config.LobbyGames = readEnvironment(lookup, LobbyGamesEnv)
		config.LobbyDatabaseURL = readEnvironment(lookup, LobbyDatabaseURLEnv)
		if err := validateURL(LobbyDatabaseURLEnv, config.LobbyDatabaseURL, "postgres", "postgresql"); err != nil {
			return Config{}, err
		}
		if err := validateURL(CentrifugoPublicURLEnv, config.CentrifugoPublicURL, "ws", "wss"); err != nil {
			return Config{}, err
		}
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
	if err := config.loadLobbyGames(lookup); err != nil {
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
