package dependencies

import (
	"net/http"

	"islandBattles/backend/observability"

	"github.com/centrifugal/gocent/v3"
)

// ConnectCentrifugoClient makes the client for Centrifugo's API. Each API call
// gets a span when the request's trace is sampled.
func ConnectCentrifugoClient(config Config) *gocent.Client {
	httpClient := &http.Client{
		Transport: observability.HTTPTransport(gocent.DefaultHTTPClient.Transport, "Centrifugo API"),
		Timeout:   gocent.DefaultHTTPClient.Timeout,
	}
	return gocent.New(gocent.Config{Addr: config.CentrifugoAPIURL, Key: config.CentrifugoAPIKey, HTTPClient: httpClient})
}
