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
