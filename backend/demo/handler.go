// Package demo serves the Island Battles browser modules that the lobby
// frontend loads: the example player, the create form's map choice and the
// waiting room's bot choice.
package demo

import (
	"embed"
	"net/http"

	"islandBattles/lobby/frontend"
)

//go:embed example-player.mjs lobby-settings.mjs bot-choice.mjs localhost-bot.mjs
var assets embed.FS

func Handler() http.Handler {
	files := http.FileServer(http.FS(assets))
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		// These are scripts only; a deploy reaches players within five minutes.
		response.Header().Set("Cache-Control", frontend.StaticAssetCacheControl)
		files.ServeHTTP(response, request)
	})
}
