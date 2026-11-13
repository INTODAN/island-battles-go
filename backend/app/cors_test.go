package app

import (
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
)

func corsRequest(handler http.Handler, method, path string, headers map[string]string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, nil)
	for name, value := range headers {
		request.Header.Set(name, value)
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}

// The game's site answers the lobby's sites with CORS, so the lobby page on
// another origin loads the game's modules and calls its API, and answers
// any other origin without.
func TestTheGameAllowsTheLobbysOrigins(t *testing.T) {
	served := 0
	game := lobbyCORS([]string{"https://lobby.example.org", "http://localhost:8890"})(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		served++
		response.WriteHeader(http.StatusTeapot)
	}))
	for _, path := range []string{"/game/demo/bot-choice.mjs", "/game/from-lobby/l/generation", "/generation/commands", "/bots/defaults", "/map/templates"} {
		response := corsRequest(game, http.MethodGet, path, map[string]string{"Origin": "https://lobby.example.org", "Authorization": "Bearer t"})
		headers := response.Header()
		if response.Code != http.StatusTeapot || headers.Get("Access-Control-Allow-Origin") != "https://lobby.example.org" ||
			headers.Get("Access-Control-Expose-Headers") != "Retry-After" || !slices.Contains(headers.Values("Vary"), "Origin") {
			t.Fatalf("GET %s: status %d, headers %v", path, response.Code, headers)
		}
	}
	// A preflight from a lobby origin is answered without reaching the route.
	before := served
	response := corsRequest(game, http.MethodOptions, "/generation/commands", map[string]string{
		"Origin": "http://localhost:8890", "Access-Control-Request-Method": "POST", "Access-Control-Request-Headers": "authorization, content-type",
	})
	headers := response.Header()
	if response.Code != http.StatusNoContent || served != before || headers.Get("Access-Control-Allow-Origin") != "http://localhost:8890" ||
		headers.Get("Access-Control-Allow-Methods") != "GET, POST, PUT, DELETE" || headers.Get("Access-Control-Allow-Headers") != "Authorization, Content-Type" ||
		headers.Get("Access-Control-Max-Age") != "600" || !slices.Contains(headers.Values("Vary"), "Origin") {
		t.Fatalf("preflight: status %d, headers %v", response.Code, headers)
	}
	// Any other origin gets no CORS headers, never *, and its preflight goes
	// on to the router.
	for _, origin := range []string{"https://evil.example.org", "https://lobby.example.org.evil.example", "null", "http://localhost:3000"} {
		for _, method := range []string{http.MethodGet, http.MethodOptions} {
			response := corsRequest(game, method, "/game/from-lobby/l/generation", map[string]string{"Origin": origin, "Access-Control-Request-Method": "GET"})
			if response.Code != http.StatusTeapot || response.Header().Get("Access-Control-Allow-Origin") != "" || response.Header().Get("Access-Control-Allow-Methods") != "" {
				t.Fatalf("%s from %s: status %d, headers %v", method, origin, response.Code, response.Header())
			}
		}
	}
	// A request without an Origin, such as a bot's, is served as before.
	if response := corsRequest(game, http.MethodGet, "/generation/commands", nil); response.Code != http.StatusTeapot || len(response.Header()) != 0 {
		t.Fatalf("no origin: status %d, headers %v", response.Code, response.Header())
	}
}

func TestWithoutLobbyOriginsNothingChanges(t *testing.T) {
	game := lobbyCORS(nil)(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) { response.WriteHeader(http.StatusTeapot) }))
	response := corsRequest(game, http.MethodOptions, "/game/demo/bot-choice.mjs", map[string]string{"Origin": "https://lobby.example.org", "Access-Control-Request-Method": "GET"})
	if response.Code != http.StatusTeapot || len(response.Header()) != 0 {
		t.Fatalf("status %d, headers %v", response.Code, response.Header())
	}
}
