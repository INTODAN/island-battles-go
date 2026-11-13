package app

import (
	"net/http"
	"slices"
)

// The CORS answer to the lobby's sites, whose lobby page loads the game's
// modules and documents and calls its API from another origin (#228).
const (
	corsAllowedMethods = "GET, POST, PUT, DELETE"
	corsAllowedHeaders = "Authorization, Content-Type"
	// Browsers keep a preflight's answer for 10 minutes.
	corsMaxAge = "600"
	// The clients wait as long as a throttled answer's Retry-After says.
	corsExposedHeaders = "Retry-After"
)

// lobbyCORS lets the lobby's sites, origins (ISLAND_BATTLES_LOBBY_ORIGINS),
// read the game's site: Access-Control-Allow-Origin names the request's
// origin when it is one of them, never *, and a preflight from one of them
// is answered with 204, the methods and the headers that the lobby page
// sends (Authorization and Content-Type), cached for 10 minutes. Any other
// origin gets no CORS headers, and its preflight goes on to the router.
// Without origins, nothing changes.
func lobbyCORS(origins []string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		if len(origins) == 0 {
			return next
		}
		return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
			origin := request.Header.Get("Origin")
			if origin == "" {
				next.ServeHTTP(response, request)
				return
			}
			headers := response.Header()
			headers.Add("Vary", "Origin")
			if !slices.Contains(origins, origin) {
				next.ServeHTTP(response, request)
				return
			}
			headers.Set("Access-Control-Allow-Origin", origin)
			if request.Method == http.MethodOptions && request.Header.Get("Access-Control-Request-Method") != "" {
				headers.Add("Vary", "Access-Control-Request-Method")
				headers.Add("Vary", "Access-Control-Request-Headers")
				headers.Set("Access-Control-Allow-Methods", corsAllowedMethods)
				headers.Set("Access-Control-Allow-Headers", corsAllowedHeaders)
				headers.Set("Access-Control-Max-Age", corsMaxAge)
				response.WriteHeader(http.StatusNoContent)
				return
			}
			headers.Set("Access-Control-Expose-Headers", corsExposedHeaders)
			next.ServeHTTP(response, request)
		})
	}
}
