// Package demo serves the example Island Battles browser player.
package demo

import (
	"embed"
	"net/http"
)

//go:embed example-player.mjs
var assets embed.FS

func Handler() http.Handler {
	files := http.FileServer(http.FS(assets))
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Cache-Control", "no-store")
		files.ServeHTTP(response, request)
	})
}
