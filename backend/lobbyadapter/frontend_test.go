package lobbyadapter_test

import (
	"testing"

	"islandBattles/backend/lobbyadapter"
	"islandBattles/lobby/frontend"
)

func TestFrontendGameIsAValidCatalogEntry(t *testing.T) {
	if _, err := frontend.Handler([]frontend.Game{lobbyadapter.FrontendGame()}); err != nil {
		t.Fatal(err)
	}
}
