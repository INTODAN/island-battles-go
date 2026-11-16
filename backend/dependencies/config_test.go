package dependencies

import (
	"strings"
	"testing"
	"time"
)

func TestLoadConfigRejectsMissingSecretsWithoutEchoingValues(t *testing.T) {
	t.Parallel()

	values := validConfigurationEnvironment()
	delete(values, LobbyHookSecretEnv)
	delete(values, LobbyURLEnv)
	delete(values, SubmitTokenSecretEnv)
	values[CentrifugoAPIKeyEnv] = "do-not-print-api-key"

	_, err := LoadConfigFromLookup(mapLookup(values))
	if err == nil {
		t.Fatal("LoadConfigFromLookup() error = nil, want missing configuration error")
	}
	if !strings.Contains(err.Error(), LobbyHookSecretEnv) || !strings.Contains(err.Error(), LobbyURLEnv) || !strings.Contains(err.Error(), SubmitTokenSecretEnv) {
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
	if config.CentrifugoAPIURL != "http://localhost:8000/api" || config.LobbyURL != "http://localhost:8890" {
		t.Fatalf("CentrifugoAPIURL = %q, LobbyURL = %q", config.CentrifugoAPIURL, config.LobbyURL)
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

// The lobby's sites that may read the game from another origin (#228).
func TestLoadConfigReadsTheLobbyOrigins(t *testing.T) {
	t.Parallel()

	values := validConfigurationEnvironment()
	if config, err := LoadConfigFromLookup(mapLookup(values)); err != nil || len(config.LobbyOrigins) != 0 {
		t.Fatalf("LobbyOrigins = %q, error = %v", config.LobbyOrigins, err)
	}
	values[LobbyOriginsEnv] = "https://lobby.example.org, http://localhost:8890"
	if config, err := LoadConfigFromLookup(mapLookup(values)); err != nil || strings.Join(config.LobbyOrigins, " ") != "https://lobby.example.org http://localhost:8890" {
		t.Fatalf("LobbyOrigins = %q, error = %v", config.LobbyOrigins, err)
	}
	for _, invalid := range []string{"*", "lobby.example.org", "https://lobby.example.org/", "https://lobby.example.org/lobby", "https://LOBBY.example.org", "https://lobby.example.org:443", "ws://lobby.example.org"} {
		values[LobbyOriginsEnv] = invalid
		if _, err := LoadConfigFromLookup(mapLookup(values)); err == nil || !strings.Contains(err.Error(), LobbyOriginsEnv) {
			t.Errorf("%q: error = %v, want %s validation error", invalid, err, LobbyOriginsEnv)
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

func TestLoadConfigReadsTheBotComputeBudgetAndFake(t *testing.T) {
	t.Parallel()

	config, err := LoadConfigFromLookup(mapLookup(validConfigurationEnvironment()))
	if err != nil || config.BotComputeBudgetGBSeconds != 300000 || config.BotComputeFakeGBSeconds != nil {
		t.Fatalf("defaults: budget %v, fake %v, %v", config.BotComputeBudgetGBSeconds, config.BotComputeFakeGBSeconds, err)
	}
	values := validConfigurationEnvironment()
	values[BotComputeBudgetEnv], values[BotComputeFakeEnv] = "1200.5", "0"
	config, err = LoadConfigFromLookup(mapLookup(values))
	if err != nil || config.BotComputeBudgetGBSeconds != 1200.5 || config.BotComputeFakeGBSeconds == nil || *config.BotComputeFakeGBSeconds != 0 {
		t.Fatalf("set: budget %v, fake %v, %v", config.BotComputeBudgetGBSeconds, config.BotComputeFakeGBSeconds, err)
	}
	for _, name := range []string{BotComputeBudgetEnv, BotComputeFakeEnv} {
		for _, invalid := range []string{"-1", "lots", "NaN", "Inf"} {
			values := validConfigurationEnvironment()
			values[name] = invalid
			if _, err := LoadConfigFromLookup(mapLookup(values)); err == nil || !strings.Contains(err.Error(), name) {
				t.Errorf("%s=%q: error = %v", name, invalid, err)
			}
		}
	}
	// The runner functions' compute is read from CloudWatch, never faked.
	values = validConfigurationEnvironment()
	values[BotRunnerPythonEnv], values[BotRunnerNativeEnv], values[BotComputeFakeEnv] = "python", "native", "1"
	if _, err := LoadConfigFromLookup(mapLookup(values)); err == nil || !strings.Contains(err.Error(), BotComputeFakeEnv) {
		t.Errorf("a fake with the runner functions: error = %v", err)
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
		DatabaseURLEnv:       "postgres://user:password@localhost:5432/island_battles?sslmode=disable",
		CentrifugoAPIURLEnv:  "http://localhost:8000/api",
		CentrifugoAPIKeyEnv:  "api-key",
		SubmitTokenSecretEnv: "submit-token-secret",
		LobbyHookSecretEnv:   testHookSecret,
		LobbyURLEnv:          "http://localhost:8890",
	}
}

// testHookSecret is a shared secret of the lobby's hooks of 32 bytes.
const testHookSecret = "hook-secret-of-thirty-two-bytes!"

func mapLookup(values map[string]string) func(string) (string, bool) {
	return func(name string) (string, bool) {
		value, ok := values[name]
		return value, ok
	}
}

// The game reaches the lobby at its URL, an origin, with a hook secret of
// at least 32 bytes.
func TestLoadConfigBindsIslandBattlesToTheLobby(t *testing.T) {
	t.Parallel()

	values := validConfigurationEnvironment()
	values[LobbyURLEnv] = "https://lobby.example.test/"
	config, err := LoadConfigFromLookup(mapLookup(values))
	if err != nil {
		t.Fatal(err)
	}
	if config.LobbyURL != "https://lobby.example.test" || string(config.LobbyHookSecret) != testHookSecret {
		t.Fatalf("lobby URL = %q, hook secret = %q", config.LobbyURL, config.LobbyHookSecret)
	}
	for name, change := range map[string]func(map[string]string){
		"no lobby URL":          func(values map[string]string) { delete(values, LobbyURLEnv) },
		"a lobby URL over http": func(values map[string]string) { values[LobbyURLEnv] = "http://lobby.example.test" },
		"a lobby URL with a path": func(values map[string]string) {
			values[LobbyURLEnv] = "https://lobby.example.test/lobby"
		},
		"no hook secret":      func(values map[string]string) { delete(values, LobbyHookSecretEnv) },
		"a short hook secret": func(values map[string]string) { values[LobbyHookSecretEnv] = "short" },
	} {
		values := validConfigurationEnvironment()
		change(values)
		if _, err := LoadConfigFromLookup(mapLookup(values)); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
}
