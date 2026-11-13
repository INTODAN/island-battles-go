// Package demo serves the Island Battles browser modules that the lobby
// frontend loads: the example player, the create form's map choice and the
// waiting room's bot choice.
package demo

import (
	"embed"
	"net/http"
)

//go:embed example-player.mjs lobby-settings.mjs bot-choice.mjs bot-packages.mjs localhost-bot.mjs
var assets embed.FS

func Handler() http.Handler {
	files := http.FileServer(http.FS(assets))
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		// These are scripts only; a deploy reaches players within five minutes.
		response.Header().Set("Cache-Control", staticAssetCacheControl)
		files.ServeHTTP(response, request)
	})
}

// staticAssetCacheControl caches the scripts and styles beside the lobby page
// for five minutes, as the lobby page caches its own, so a deploy reaches
// players within five minutes without fingerprinted file names.
const staticAssetCacheControl = "public, max-age=300"
