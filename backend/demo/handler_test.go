package demo_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"islandBattles/backend/demo"
)

func TestHandlerServesTheModulesCached(t *testing.T) {
	for _, module := range []string{"example-player.mjs", "lobby-settings.mjs", "bot-choice.mjs", "localhost-bot.mjs"} {
		recorder := httptest.NewRecorder()
		http.StripPrefix("/game/demo/", demo.Handler()).ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/game/demo/"+module, nil))
		if recorder.Code != http.StatusOK {
			t.Fatalf("%s: status %d", module, recorder.Code)
		}
		if got := recorder.Header().Get("Cache-Control"); got != "public, max-age=300" {
			t.Errorf("%s: Cache-Control %q", module, got)
		}
	}
}

// During a deploy a new page can ask an old instance for a module it lacks.
// Go's file server drops Cache-Control from an error answer, so the 404 is
// never kept.
func TestHandlerNeverCachesAMissingModule(t *testing.T) {
	recorder := httptest.NewRecorder()
	http.StripPrefix("/game/demo/", demo.Handler()).ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/game/demo/missing.mjs", nil))
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status %d", recorder.Code)
	}
	if got := recorder.Header().Get("Cache-Control"); got != "" {
		t.Errorf("Cache-Control %q", got)
	}
}
