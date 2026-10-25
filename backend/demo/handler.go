// Package demo serves the Island Battles browser modules that the lobby
// frontend loads: the example player and the create form's map choice.
package demo

import (
	"embed"
	"net/http"
)

//go:embed example-player.mjs lobby-settings.mjs
var assets embed.FS

func Handler() http.Handler {
	files := http.FileServer(http.FS(assets))
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Cache-Control", "no-store")
		files.ServeHTTP(response, request)
	})
}
