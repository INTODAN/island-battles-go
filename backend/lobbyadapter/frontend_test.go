package lobbyadapter_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"islandBattles/backend/demo"
	"islandBattles/backend/demoviewer"
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

// The catalog links the demo viewer where app.go mounts it.
func TestFrontendGameLinksTheDemoViewer(t *testing.T) {
	links := lobbyadapter.FrontendGame().Links
	if len(links) != 2 || links[0].Label != "Watch a saved demo" {
		t.Fatalf("links = %+v", links)
	}
	viewer, err := demoviewer.Handler()
	if err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	http.StripPrefix("/game/demo-viewer/", viewer).ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, links[0].URL, nil))
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), "Open demo file") {
		t.Fatalf("GET %s: status %d", links[0].URL, recorder.Code)
	}
}

// The web client is served at /play/ by CloudFront, not the backend
// (deploy/README.md, "The web client").
func TestFrontendGameLinksThe3DClient(t *testing.T) {
	links := lobbyadapter.FrontendGame().Links
	if len(links) != 2 || links[1].Label != "Play in 3D" || links[1].URL != "/play/" {
		t.Fatalf("links = %+v", links)
	}
}
