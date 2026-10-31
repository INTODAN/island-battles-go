package demoviewer_test

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"islandBattles/backend/demoviewer"
	"islandBattles/backend/models"
	"islandBattles/backend/services"
)

func get(t *testing.T, path string) *httptest.ResponseRecorder {
	t.Helper()
	handler, err := demoviewer.Handler()
	if err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	http.StripPrefix("/game/demo-viewer/", handler).ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
	return recorder
}

func TestTheViewerIsTheReplayPageWithoutAGame(t *testing.T) {
	response := get(t, "/game/demo-viewer/")
	if response.Code != http.StatusOK || !strings.HasPrefix(response.Header().Get("Content-Type"), "text/html") || response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("page: %d %v", response.Code, response.Header())
	}
	page, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	viewer, err := services.RenderDemoViewer()
	if err != nil {
		t.Fatal(err)
	}
	replay, err := services.RenderGameReplay(services.GameReplay{GameID: 1, Teams: []models.GameTeam{}, Frames: []models.GenerationView{{ID: 1}}})
	if err != nil {
		t.Fatal(err)
	}
	if string(page) != viewer {
		t.Fatal("the page is not the rendered viewer")
	}
	// Both pages draw with the same script, and only the viewer opens files.
	for _, shared := range []string{"function renderClashes(frame, animate)", "function renderLogs(logs)", `id="previous"`, `id="next"`, `id="timeline"`} {
		if !strings.Contains(viewer, shared) || !strings.Contains(replay, shared) {
			t.Errorf("%q is not on both pages", shared)
		}
	}
	for _, own := range []string{`id="open-demo"`, `<script type="module" src="./viewer.mjs">`, "window.showDemo",
		fmt.Sprintf(`data-format="%s" data-schema-version="%d"`, models.DemoFormat, models.DemoSchemaVersion)} {
		if !strings.Contains(viewer, own) || strings.Contains(replay, own) {
			t.Errorf("%q is not on the viewer alone", own)
		}
	}
	// html/template writes a nil replay as null, with spaces around it.
	if !regexp.MustCompile(`let replay = \s*null\s*;`).MatchString(viewer) {
		t.Error("the viewer embeds a replay")
	}
	// The replay saves demos; the viewer only opens them.
	if !strings.Contains(replay, `id="save-demo"`) || strings.Contains(viewer, `id="save-demo"`) {
		t.Error("only the replay should save demos")
	}
}

func TestTheViewerServesItsModulesAndNothingElse(t *testing.T) {
	for _, module := range []string{"viewer.mjs", "demo-file.mjs"} {
		response := get(t, "/game/demo-viewer/"+module)
		// Browsers load ES modules only with a JavaScript content type.
		if response.Code != http.StatusOK || !strings.HasPrefix(response.Header().Get("Content-Type"), "text/javascript") {
			t.Errorf("%s: %d %q", module, response.Code, response.Header().Get("Content-Type"))
		}
	}
	for _, path := range []string{"/game/demo-viewer/handler.go", "/game/demo-viewer/demo-file.test.mjs", "/game/demo-viewer/missing.mjs"} {
		if response := get(t, path); response.Code != http.StatusNotFound {
			t.Errorf("%s: %d", path, response.Code)
		}
	}
}
