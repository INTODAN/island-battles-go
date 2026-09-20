package dependencies

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"sort"
	"strings"
)

const (
	DatabaseURLEnv               = "ISLAND_BATTLES_DATABASE_URL"
	HTTPAddrEnv                  = "ISLAND_BATTLES_HTTP_ADDR"
	CentrifugoPublicURLEnv       = "ISLAND_BATTLES_CENTRIFUGO_PUBLIC_URL"
	CentrifugoAPIURLEnv          = "ISLAND_BATTLES_CENTRIFUGO_API_URL"
	CentrifugoAPIKeyEnv          = "ISLAND_BATTLES_CENTRIFUGO_API_KEY"
	ConnectionTokenHMACSecretEnv = "ISLAND_BATTLES_CONNECTION_TOKEN_HMAC_SECRET"
	LobbyProxySecretEnv          = "ISLAND_BATTLES_LOBBY_PROXY_SECRET"
)

// Config contains bootstrap settings that must be supplied explicitly by the
// deployment. Values are loaded only by the executable, never by package init.
type Config struct {
	DatabaseURL               string
	HTTPAddr                  string
	CentrifugoPublicURL       string
	CentrifugoAPIURL          string
	CentrifugoAPIKey          string
	ConnectionTokenHMACSecret string
	LobbyProxySecret          string
}

// LoadConfig reads and validates the backend's required environment values.
func LoadConfig() (Config, error) {
	return LoadConfigFromLookup(os.LookupEnv)
}

// LoadConfigFromLookup makes configuration validation deterministic in tests.
func LoadConfigFromLookup(lookup func(string) (string, bool)) (Config, error) {
	config := Config{
		DatabaseURL:               readEnvironment(lookup, DatabaseURLEnv),
		HTTPAddr:                  readEnvironment(lookup, HTTPAddrEnv),
		CentrifugoPublicURL:       readEnvironment(lookup, CentrifugoPublicURLEnv),
		CentrifugoAPIURL:          readEnvironment(lookup, CentrifugoAPIURLEnv),
		CentrifugoAPIKey:          readEnvironment(lookup, CentrifugoAPIKeyEnv),
		ConnectionTokenHMACSecret: readEnvironment(lookup, ConnectionTokenHMACSecretEnv),
		LobbyProxySecret:          readEnvironment(lookup, LobbyProxySecretEnv),
	}

	if missing := config.missingEnvironmentNames(); len(missing) > 0 {
		sort.Strings(missing)
		return Config{}, fmt.Errorf("missing required environment variables: %s", strings.Join(missing, ", "))
	}
	if err := config.validate(); err != nil {
		return Config{}, err
	}
	return config, nil
}

func readEnvironment(lookup func(string) (string, bool), name string) string {
	value, ok := lookup(name)
	if !ok {
		return ""
	}
	return strings.TrimSpace(value)
}

func (config Config) missingEnvironmentNames() []string {
	values := map[string]string{
		DatabaseURLEnv:               config.DatabaseURL,
		HTTPAddrEnv:                  config.HTTPAddr,
		CentrifugoPublicURLEnv:       config.CentrifugoPublicURL,
		CentrifugoAPIURLEnv:          config.CentrifugoAPIURL,
		CentrifugoAPIKeyEnv:          config.CentrifugoAPIKey,
		ConnectionTokenHMACSecretEnv: config.ConnectionTokenHMACSecret,
		LobbyProxySecretEnv:          config.LobbyProxySecret,
	}

	missing := make([]string, 0)
	for name, value := range values {
		if value == "" {
			missing = append(missing, name)
		}
	}
	return missing
}

func (config Config) validate() error {
	if _, _, err := net.SplitHostPort(config.HTTPAddr); err != nil {
		return fmt.Errorf("%s must be a host:port address: %w", HTTPAddrEnv, err)
	}
	if err := validateURL(DatabaseURLEnv, config.DatabaseURL, "postgres", "postgresql"); err != nil {
		return err
	}
	if err := validateURL(CentrifugoPublicURLEnv, config.CentrifugoPublicURL, "ws", "wss"); err != nil {
		return err
	}
	return validateURL(CentrifugoAPIURLEnv, config.CentrifugoAPIURL, "http", "https")
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
