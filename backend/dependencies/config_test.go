package dependencies

import (
	"strings"
	"testing"
)

func TestLoadConfigRejectsMissingSecretsWithoutEchoingValues(t *testing.T) {
	t.Parallel()

	values := validConfigurationEnvironment()
	delete(values, ConnectionTokenHMACSecretEnv)
	delete(values, LobbyProxySecretEnv)
	values[CentrifugoAPIKeyEnv] = "do-not-print-api-key"

	_, err := LoadConfigFromLookup(mapLookup(values))
	if err == nil {
		t.Fatal("LoadConfigFromLookup() error = nil, want missing configuration error")
	}
	if !strings.Contains(err.Error(), ConnectionTokenHMACSecretEnv) || !strings.Contains(err.Error(), LobbyProxySecretEnv) {
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

func validConfigurationEnvironment() map[string]string {
	return map[string]string{
		DatabaseURLEnv:               "postgres://user:password@localhost:5432/island_battles?sslmode=disable",
		HTTPAddrEnv:                  ":8888",
		CentrifugoPublicURLEnv:       "ws://localhost:8000/connection/websocket",
		CentrifugoAPIURLEnv:          "http://localhost:8000/api",
		CentrifugoAPIKeyEnv:          "api-key",
		ConnectionTokenHMACSecretEnv: "connection-token-secret",
		LobbyProxySecretEnv:          "proxy-secret",
	}
}

func mapLookup(values map[string]string) func(string) (string, bool) {
	return func(name string) (string, bool) {
		value, ok := values[name]
		return value, ok
	}
}
