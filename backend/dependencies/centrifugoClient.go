package dependencies

import "github.com/centrifugal/gocent/v3"

func ConnectCentrifugoClient(config Config) *gocent.Client {
	client := gocent.New(gocent.Config{Addr: config.CentrifugoAPIURL, Key: config.CentrifugoAPIKey})
	return client
}
