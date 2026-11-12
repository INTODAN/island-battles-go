package dependencies

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"islandBattles/backend/retention"
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
// defaults to each request's own. Values are loaded only by
// executables, never by package init.
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
}

// LoadConfig reads and validates the HTTP API's environment values.
func LoadConfig() (Config, error) {
	return LoadConfigFromLookup(os.LookupEnv)
}

// LoadConfigFromLookup makes configuration validation deterministic in tests.
func LoadConfigFromLookup(lookup func(string) (string, bool)) (Config, error) {
	if err := requireEnvironment(lookup, DatabaseURLEnv, CentrifugoPublicURLEnv, CentrifugoAPIURLEnv,
		CentrifugoAPIKeyEnv, ConnectionTokenHMACSecretEnv, LobbyProxySecretEnv, SubmitTokenSecretEnv); err != nil {
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
	config := Config{
		Database:                  database,
		CentrifugoPublicURL:       readEnvironment(lookup, CentrifugoPublicURLEnv),
		CentrifugoAPIURL:          readEnvironment(lookup, CentrifugoAPIURLEnv),
		CentrifugoAPIKey:          readEnvironment(lookup, CentrifugoAPIKeyEnv),
		ConnectionTokenHMACSecret: readEnvironment(lookup, ConnectionTokenHMACSecretEnv),
		LobbyProxySecret:          readEnvironment(lookup, LobbyProxySecretEnv),
		SubmitTokenSecret:         readEnvironment(lookup, SubmitTokenSecretEnv),
		PublicURL:                 strings.TrimRight(readEnvironment(lookup, PublicURLEnv), "/"),
		AdminAPIKey:               readEnvironment(lookup, AdminAPIKeyEnv),
		GameRetentionPeriod:       gameRetentionPeriod,
		BotPackagesBucket:         readEnvironment(lookup, BotPackagesBucketEnv),
	}
	if err := validateURL(CentrifugoPublicURLEnv, config.CentrifugoPublicURL, "ws", "wss"); err != nil {
		return Config{}, err
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
