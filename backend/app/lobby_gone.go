package app

import (
	"encoding/json"
	"net/http"
)

// PlayPagePath is where the deployment serves the 3D web client, on the
// game's own origin. The game's / redirects to it (#279).
const PlayPagePath = "/play/"

// lobbyGone answers every request under /lobby on the game's origin, where
// the lobby ran in the game's process until it moved to its own (#232), with
// 410 Gone and a problem (RFC 9457) that names the lobby's URL, so an old
// client fails clearly. In production, old links to the lobby's page are
// redirected to the lobby before they reach the backend, by the game's
// CloudFront distribution (deploy/terraform/cloudfront.tf).
func lobbyGone(lobbyURL string) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		body, err := json.Marshal(struct {
			Title    string `json:"title"`
			Status   int    `json:"status"`
			Detail   string `json:"detail"`
			LobbyURL string `json:"lobby_url"`
		}{
			Title:  "Gone",
			Status: http.StatusGone,
			Detail: "The lobby moved to " + lobbyURL + ". Call " + lobbyURL + request.URL.Path +
				" instead, and upgrade island-battles-python or the Go SDK, which take the lobby's URL apart from the game's.",
			LobbyURL: lobbyURL,
		})
		if err != nil {
			http.Error(response, "The lobby moved to "+lobbyURL, http.StatusGone)
			return
		}
		response.Header().Set("Content-Type", "application/problem+json")
		response.Header().Set("Cache-Control", "no-store")
		response.WriteHeader(http.StatusGone)
		_, _ = response.Write(body)
	}
}
