package lobbyadapter_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"islandBattles/backend/demo"
	"islandBattles/backend/lobbyadapter"
	"islandBattles/lobby/frontend"
)

func TestFrontendGameIsAValidCatalogEntry(t *testing.T) {
	if _, err := frontend.Handler([]frontend.Game{lobbyadapter.FrontendGame()}); err != nil {
		t.Fatal(err)
	}
}

// The catalog's modules are served where main.go mounts the demo handler.
func TestFrontendGameModulesAreServed(t *testing.T) {
	game := lobbyadapter.FrontendGame()
	handler := http.StripPrefix("/game/demo/", demo.Handler())
	for _, module := range []string{game.ExamplePlayer.Module, game.SettingsModule} {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, module, nil))
		if recorder.Code != http.StatusOK || !strings.Contains(recorder.Header().Get("Content-Type"), "javascript") {
			t.Errorf("GET %s: status %d, content type %q", module, recorder.Code, recorder.Header().Get("Content-Type"))
		}
	}
}
