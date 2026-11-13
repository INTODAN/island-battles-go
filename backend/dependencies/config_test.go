package dependencies

import (
	"encoding/json"
	"islandBattles/backend/lobbyadapter"
	"strings"
	"testing"
	"time"
)

func TestLoadConfigRejectsMissingSecretsWithoutEchoingValues(t *testing.T) {
	t.Parallel()

	values := validConfigurationEnvironment()
	delete(values, ConnectionTokenHMACSecretEnv)
	delete(values, LobbyProxySecretEnv)
	delete(values, SubmitTokenSecretEnv)
	values[CentrifugoAPIKeyEnv] = "do-not-print-api-key"

	_, err := LoadConfigFromLookup(mapLookup(values))
	if err == nil {
		t.Fatal("LoadConfigFromLookup() error = nil, want missing configuration error")
	}
	if !strings.Contains(err.Error(), ConnectionTokenHMACSecretEnv) || !strings.Contains(err.Error(), LobbyProxySecretEnv) || !strings.Contains(err.Error(), SubmitTokenSecretEnv) {
		t.Fatalf("LoadConfigFromLookup() error = %q, want missing secret variable names", err)
	}
	if strings.Contains(err.Error(), values[CentrifugoAPIKeyEnv]) {
		t.Fatalf("LoadConfigFromLookup() error leaked a secret: %q", err)
	}
}

func TestLoadConfigAcceptsExplicitValidConfiguration(t *testing.T) {
	t.Parallel()

	config, err := LoadConfigFromLookup(mapLookup(validConfigurationEnvironment()))
	if err != nil {
		t.Fatalf("LoadConfigFromLookup() error = %v", err)
	}
	if config.CentrifugoPublicURL != "ws://localhost:8000/connection/websocket" {
		t.Fatalf("CentrifugoPublicURL = %q", config.CentrifugoPublicURL)
	}
	if config.AdminAPIKey != "" {
		t.Fatalf("AdminAPIKey = %q without %s", config.AdminAPIKey, AdminAPIKeyEnv)
	}
	if config.SubmitTokenSecret != "submit-token-secret" || config.PublicURL != "" {
		t.Fatalf("SubmitTokenSecret = %q, PublicURL = %q", config.SubmitTokenSecret, config.PublicURL)
	}
	environment := validConfigurationEnvironment()
	environment[AdminAPIKeyEnv] = " admin-key "
	if config, err := LoadConfigFromLookup(mapLookup(environment)); err != nil || config.AdminAPIKey != "admin-key" {
		t.Fatalf("AdminAPIKey = %q, error = %v", config.AdminAPIKey, err)
	}
}

func TestLoadConfigReadsThePlayerTokenKeys(t *testing.T) {
	t.Parallel()

	values := validConfigurationEnvironment()
	values[PlayerTokenKeysEnv] = "new:" + testSeed + ",old:" + testSeed
	config, err := LoadConfigFromLookup(mapLookup(values))
	if err != nil {
		t.Fatal(err)
	}
	if keys := config.PlayerTokenKeys.JWKS().Keys; len(keys) != 2 || keys[0].KeyID != "new" {
		t.Fatalf("keys = %+v", keys)
	}
	if config.PlayerTokenLifetime != 15*time.Minute || config.LobbyIssuer() != LocalLobbyIssuer {
		t.Fatalf("lifetime = %v, issuer = %q", config.PlayerTokenLifetime, config.LobbyIssuer())
	}
	values[PublicURLEnv] = "https://island-battles.example.org"
	values[PlayerTokenLifetimeEnv] = "2m"
	if config, err := LoadConfigFromLookup(mapLookup(values)); err != nil || config.PlayerTokenLifetime != 2*time.Minute || config.LobbyIssuer() != "https://island-battles.example.org" {
		t.Fatalf("lifetime = %v, issuer = %q, error = %v", config.PlayerTokenLifetime, config.LobbyIssuer(), err)
	}
	for _, lifetime := range []string{"30s", "16m", "soon"} {
		values[PlayerTokenLifetimeEnv] = lifetime
		if _, err := LoadConfigFromLookup(mapLookup(values)); err == nil || !strings.Contains(err.Error(), PlayerTokenLifetimeEnv) {
			t.Errorf("lifetime %q: error = %v", lifetime, err)
		}
	}
	delete(values, PlayerTokenLifetimeEnv)
	for _, keys := range []string{"", "k1", "k1:" + testSeed[:20], "k1:" + testSeed + ",k1:" + testSeed} {
		values[PlayerTokenKeysEnv] = keys
		_, err := LoadConfigFromLookup(mapLookup(values))
		if err == nil || !strings.Contains(err.Error(), PlayerTokenKeysEnv) || (len(keys) > 10 && strings.Contains(err.Error(), keys[3:])) {
			t.Errorf("keys %q: error = %v", keys, err)
		}
	}
}

func TestLoadConfigReadsThePublicURL(t *testing.T) {
	t.Parallel()

	values := validConfigurationEnvironment()
	values[PublicURLEnv] = "https://island-battles.example.org/"
	if config, err := LoadConfigFromLookup(mapLookup(values)); err != nil || config.PublicURL != "https://island-battles.example.org" {
		t.Fatalf("PublicURL = %q, error = %v", config.PublicURL, err)
	}
	for _, invalid := range []string{"island-battles.example.org", "ws://island-battles.example.org", "https://island-battles.example.org/play"} {
		values[PublicURLEnv] = invalid
		if _, err := LoadConfigFromLookup(mapLookup(values)); err == nil || !strings.Contains(err.Error(), PublicURLEnv) {
			t.Errorf("%q: error = %v, want %s validation error", invalid, err, PublicURLEnv)
		}
	}
}

// Hosted and uploaded bots are on unless ISLAND_BATTLES_SERVER_BOTS is off
// (#225).
func TestLoadConfigReadsTheServerBotsFlag(t *testing.T) {
	t.Parallel()

	for value, want := range map[string]bool{"": true, "off": false, "on": true} {
		values := validConfigurationEnvironment()
		if value != "" {
			values[ServerBotsEnv] = value
		}
		if config, err := LoadConfigFromLookup(mapLookup(values)); err != nil || config.ServerBots != want {
			t.Errorf("%s=%q: ServerBots = %v, error = %v", ServerBotsEnv, value, config.ServerBots, err)
		}
	}
	for _, invalid := range []string{"yes", "true", "1", "ON"} {
		values := validConfigurationEnvironment()
		values[ServerBotsEnv] = invalid
		if _, err := LoadConfigFromLookup(mapLookup(values)); err == nil || !strings.Contains(err.Error(), ServerBotsEnv) {
			t.Errorf("%s=%q: error = %v", ServerBotsEnv, invalid, err)
		}
	}
}

func TestLoadConfigReadsBothBotRunnerFunctionsOrNeither(t *testing.T) {
	t.Parallel()

	config, err := LoadConfigFromLookup(mapLookup(validConfigurationEnvironment()))
	if err != nil || config.BotRunnerPython != "" || config.BotRunnerNative != "" {
		t.Fatalf("without the settings: %+v, %v", config, err)
	}
	values := validConfigurationEnvironment()
	values[BotRunnerPythonEnv], values[BotRunnerNativeEnv] = "island-battles-bot-runner-python", "island-battles-bot-runner-native"
	config, err = LoadConfigFromLookup(mapLookup(values))
	if err != nil || config.BotRunnerPython != "island-battles-bot-runner-python" || config.BotRunnerNative != "island-battles-bot-runner-native" {
		t.Fatalf("with both: python %q, native %q, %v", config.BotRunnerPython, config.BotRunnerNative, err)
	}
	for _, name := range []string{BotRunnerPythonEnv, BotRunnerNativeEnv} {
		values := validConfigurationEnvironment()
		values[name] = "one-runner"
		if _, err := LoadConfigFromLookup(mapLookup(values)); err == nil || !strings.Contains(err.Error(), "or neither") {
			t.Errorf("only %s: error = %v", name, err)
		}
	}
}

func TestLoadLocalBotPythonSplitsTheCommand(t *testing.T) {
	t.Parallel()

	if command := LoadLocalBotPython(mapLookup(map[string]string{})); len(command) != 1 || command[0] != "python3.14" {
		t.Errorf("default = %q", command)
	}
	command := LoadLocalBotPython(mapLookup(map[string]string{LocalBotPythonEnv: " uv run --python 3.14 python "}))
	if strings.Join(command, "|") != "uv|run|--python|3.14|python" {
		t.Errorf("command = %q", command)
	}
}

func TestLoadConfigReadsTheBotPackageBucket(t *testing.T) {
	t.Parallel()

	values := validConfigurationEnvironment()
	if config, err := LoadConfigFromLookup(mapLookup(values)); err != nil || config.BotPackagesBucket != "" {
		t.Fatalf("BotPackagesBucket = %q without %s, error = %v", config.BotPackagesBucket, BotPackagesBucketEnv, err)
	}
	values[BotPackagesBucketEnv] = " island-battles-bot-packages "
	if config, err := LoadConfigFromLookup(mapLookup(values)); err != nil || config.BotPackagesBucket != "island-battles-bot-packages" {
		t.Fatalf("BotPackagesBucket = %q, error = %v", config.BotPackagesBucket, err)
	}
	if directory := LoadBotPackagesDir(mapLookup(map[string]string{BotPackagesDirEnv: "/srv/packages"})); directory != "/srv/packages" {
		t.Fatalf("directory = %q", directory)
	}
	if directory := LoadBotPackagesDir(mapLookup(map[string]string{})); !strings.HasSuffix(directory, "island-battles-bot-packages") {
		t.Fatalf("default directory = %q", directory)
	}
}

func TestLoadConfigRejectsInvalidPublicWebSocketURL(t *testing.T) {
	t.Parallel()

	values := validConfigurationEnvironment()
	values[CentrifugoPublicURLEnv] = "http://localhost:8000/connection/websocket"

	_, err := LoadConfigFromLookup(mapLookup(values))
	if err == nil || !strings.Contains(err.Error(), CentrifugoPublicURLEnv) {
		t.Fatalf("LoadConfigFromLookup() error = %v, want %s validation error", err, CentrifugoPublicURLEnv)
	}
}

func TestLoadConfigDefaultsTheDatabasePool(t *testing.T) {
	t.Parallel()

	config, err := LoadConfigFromLookup(mapLookup(validConfigurationEnvironment()))
	if err != nil {
		t.Fatalf("LoadConfigFromLookup() error = %v", err)
	}
	if config.Database != DefaultDatabaseConfig(validConfigurationEnvironment()[DatabaseURLEnv]) {
		t.Fatalf("Database = %+v, want the default pool", config.Database)
	}
}

func TestLoadDatabaseConfigReadsPoolLimits(t *testing.T) {
	t.Parallel()

	values := map[string]string{
		DatabaseURLEnv:                   "postgres://user:password@db.example.com:5432/island_battles?sslmode=require",
		DatabaseMaxOpenConnectionsEnv:    "5",
		DatabaseMaxIdleConnectionsEnv:    "0",
		DatabaseConnectionMaxIdleTimeEnv: "30s",
	}
	config, err := LoadDatabaseConfigFromLookup(mapLookup(values))
	if err != nil {
		t.Fatalf("LoadDatabaseConfigFromLookup() error = %v", err)
	}
	if config.MaxOpenConnections != 5 || config.MaxIdleConnections != 0 || config.ConnectionMaxIdleTime != 30*time.Second {
		t.Fatalf("Database = %+v", config)
	}
	// Lowering only the open limit lowers the default idle limit with it.
	delete(values, DatabaseMaxIdleConnectionsEnv)
	values[DatabaseMaxOpenConnectionsEnv] = "1"
	if config, err = LoadDatabaseConfigFromLookup(mapLookup(values)); err != nil || config.MaxIdleConnections != 1 {
		t.Fatalf("Database = %+v, error = %v", config, err)
	}
}

func TestLoadDatabaseConfigRejectsInvalidPoolLimits(t *testing.T) {
	t.Parallel()

	for name, overrides := range map[string]map[string]string{
		"no open connections":    {DatabaseMaxOpenConnectionsEnv: "0"},
		"not a number":           {DatabaseMaxOpenConnectionsEnv: "two"},
		"more idle than open":    {DatabaseMaxOpenConnectionsEnv: "2", DatabaseMaxIdleConnectionsEnv: "3"},
		"negative idle":          {DatabaseMaxIdleConnectionsEnv: "-1"},
		"idle time not a time":   {DatabaseConnectionMaxIdleTimeEnv: "60"},
		"idle time not positive": {DatabaseConnectionMaxIdleTimeEnv: "0s"},
	} {
		values := map[string]string{DatabaseURLEnv: "postgres://user:password@localhost:5432/island_battles"}
		for key, value := range overrides {
			values[key] = value
		}
		if _, err := LoadDatabaseConfigFromLookup(mapLookup(values)); err == nil {
			t.Errorf("%s: LoadDatabaseConfigFromLookup() error = nil", name)
		}
	}
}

func TestLoadConfigReadsTheGameRetentionPeriod(t *testing.T) {
	t.Parallel()

	values := validConfigurationEnvironment()
	config, err := LoadConfigFromLookup(mapLookup(values))
	if err != nil || config.GameRetentionPeriod != 7*24*time.Hour {
		t.Fatalf("GameRetentionPeriod = %v, error = %v, want 7 days by default", config.GameRetentionPeriod, err)
	}
	values[GameRetentionPeriodEnv] = "36h"
	if config, err = LoadConfigFromLookup(mapLookup(values)); err != nil || config.GameRetentionPeriod != 36*time.Hour {
		t.Fatalf("GameRetentionPeriod = %v, error = %v, want 36h", config.GameRetentionPeriod, err)
	}
	for _, invalid := range []string{"7d", "0s", "-1h"} {
		values[GameRetentionPeriodEnv] = invalid
		if _, err := LoadConfigFromLookup(mapLookup(values)); err == nil || !strings.Contains(err.Error(), GameRetentionPeriodEnv) {
			t.Errorf("%s=%q: error = %v, want a %s validation error", GameRetentionPeriodEnv, invalid, err, GameRetentionPeriodEnv)
		}
	}
}

// Only the HTTP server host listens on an address; Lambda has none.
func TestHTTPAddrIsOnlyNeededByTheServerHost(t *testing.T) {
	t.Parallel()

	if _, err := LoadConfigFromLookup(mapLookup(validConfigurationEnvironment())); err != nil {
		t.Fatalf("LoadConfigFromLookup() without %s error = %v", HTTPAddrEnv, err)
	}
	if _, err := LoadHTTPAddrFromLookup(mapLookup(map[string]string{})); err == nil || !strings.Contains(err.Error(), HTTPAddrEnv) {
		t.Fatalf("LoadHTTPAddrFromLookup() error = %v, want missing %s", err, HTTPAddrEnv)
	}
	if _, err := LoadHTTPAddrFromLookup(mapLookup(map[string]string{HTTPAddrEnv: "8888"})); err == nil {
		t.Fatal("LoadHTTPAddrFromLookup() accepted an address without a port separator")
	}
	if address, err := LoadHTTPAddrFromLookup(mapLookup(map[string]string{HTTPAddrEnv: ":8888"})); err != nil || address != ":8888" {
		t.Fatalf("LoadHTTPAddrFromLookup() = %q, %v", address, err)
	}
}

func validConfigurationEnvironment() map[string]string {
	return map[string]string{
		DatabaseURLEnv:               "postgres://user:password@localhost:5432/island_battles?sslmode=disable",
		CentrifugoPublicURLEnv:       "ws://localhost:8000/connection/websocket",
		CentrifugoAPIURLEnv:          "http://localhost:8000/api",
		CentrifugoAPIKeyEnv:          "api-key",
		ConnectionTokenHMACSecretEnv: "connection-token-secret",
		LobbyProxySecretEnv:          "proxy-secret",
		SubmitTokenSecretEnv:         "submit-token-secret",
		PlayerTokenKeysEnv:           "k1:" + testSeed,
		LobbyHookSecretEnv:           testHookSecret,
		lobbyadapter.SecretEnv:       testHookSecret,
	}
}

// testHookSecret is a shared secret of the lobby's hooks of 32 bytes.
const testHookSecret = "hook-secret-of-thirty-two-bytes!"

// testSeed is an Ed25519 seed of 32 bytes in base64url: 32 times "s".
const testSeed = "c3Nzc3Nzc3Nzc3Nzc3Nzc3Nzc3Nzc3Nzc3Nzc3Nzc3M"

func mapLookup(values map[string]string) func(string) (string, bool) {
	return func(name string) (string, bool) {
		value, ok := values[name]
		return value, ok
	}
}

func TestLoadConfigBindsTheLobbyToIslandBattles(t *testing.T) {
	config, err := LoadConfigFromLookup(mapLookup(validConfigurationEnvironment()))
	if err != nil {
		t.Fatal(err)
	}
	if string(config.LobbyHookSecret) != testHookSecret || config.LobbyURL != "" || config.LobbyGameSecrets[lobbyadapter.SecretEnv] != testHookSecret {
		t.Fatalf("config = %+v", config)
	}
	// Without LOBBY_GAMES, the lobby calls Island Battles alone, in this
	// process.
	games, err := config.LobbyGamesConfig()
	if err != nil {
		t.Fatal(err)
	}
	var entries []struct {
		Key      string `json:"key"`
		HooksURL string `json:"hooks_url"`
	}
	if err := json.Unmarshal([]byte(games), &entries); err != nil || len(entries) != 1 || entries[0].Key != lobbyadapter.GameKey || entries[0].HooksURL != "http://localhost"+lobbyadapter.HooksPath {
		t.Fatalf("LOBBY_GAMES = %s, %v", games, err)
	}

	// Another game is added to Island Battles; a public URL names the hooks.
	values := validConfigurationEnvironment()
	values[PublicURLEnv] = "https://island-battles.example.test"
	values[LobbyGamesEnv] = `[{"key":"mock-game","name":"Mock game","hooks_url":"https://mock.example.test/hooks","secret_env":"MOCK_SECRET"}]`
	values["MOCK_SECRET"] = strings.Repeat("m", 32)
	config, err = LoadConfigFromLookup(mapLookup(values))
	if err != nil {
		t.Fatal(err)
	}
	if games, err = config.LobbyGamesConfig(); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(games), &entries); err != nil || len(entries) != 2 || entries[0].Key != "mock-game" || entries[1].HooksURL != "https://island-battles.example.test"+lobbyadapter.HooksPath {
		t.Fatalf("LOBBY_GAMES = %s, %v", games, err)
	}

	// LOBBY_GAMES that lists Island Battles is taken as it is.
	values[LobbyGamesEnv] = `[{"key":"island-battles","name":"Island Battles","hooks_url":"https://game.example.test/game/lobby-hooks","secret_env":"` + lobbyadapter.SecretEnv + `"}]`
	if config, err = LoadConfigFromLookup(mapLookup(values)); err != nil {
		t.Fatal(err)
	}
	if games, err = config.LobbyGamesConfig(); err != nil || games != values[LobbyGamesEnv] {
		t.Fatalf("LOBBY_GAMES = %s, %v", games, err)
	}

	for name, change := range map[string]func(map[string]string){
		"no hook secret":              func(values map[string]string) { delete(values, LobbyHookSecretEnv) },
		"a short hook secret":         func(values map[string]string) { values[LobbyHookSecretEnv] = "short" },
		"no lobby secret":             func(values map[string]string) { delete(values, lobbyadapter.SecretEnv) },
		"secrets that differ":         func(values map[string]string) { values[LobbyHookSecretEnv] = strings.Repeat("x", 32) },
		"LOBBY_GAMES that isn't JSON": func(values map[string]string) { values[LobbyGamesEnv] = "island-battles" },
		"a game without its secret": func(values map[string]string) {
			values[LobbyGamesEnv] = `[{"key":"mock-game","name":"Mock game","hooks_url":"https://mock.example.test/hooks","secret_env":"MOCK_SECRET"}]`
		},
		"a lobby URL over http": func(values map[string]string) { values[LobbyURLEnv] = "http://lobby.example.test" },
	} {
		values := validConfigurationEnvironment()
		change(values)
		if _, err := LoadConfigFromLookup(mapLookup(values)); err == nil {
			t.Errorf("%s: no error", name)
		}
	}

	// A lobby of its own may have its own secret for Island Battles.
	values = validConfigurationEnvironment()
	values[LobbyURLEnv] = "https://lobby.example.test/"
	values[LobbyHookSecretEnv] = strings.Repeat("x", 32)
	values[PublicURLEnv] = "https://island-battles.example.test"
	if config, err = LoadConfigFromLookup(mapLookup(values)); err != nil || config.LobbyURL != "https://lobby.example.test" {
		t.Fatalf("lobby URL = %q, %v", config.LobbyURL, err)
	}
}
