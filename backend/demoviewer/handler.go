// Package demoviewer serves the Island Battles demo viewer, a public page that
// opens a demo file saved from a game's replay and plays it. The page is the
// replay's own template without a game, so both draw a game with the same
// code; this package adds the modules that read and check the file.
package demoviewer

import (
	"embed"
	"net/http"
	"strings"
	"time"

	"islandBattles/backend/services"
)

//go:embed viewer.mjs demo-file.mjs
var modules embed.FS

// Handler serves the page and its modules. Mount it with its prefix
// stripped. The page reads demo files in the browser and calls no API, so it
// needs no authentication.
func Handler() (http.Handler, error) {
	page, err := services.RenderDemoViewer()
	if err != nil {
		return nil, err
	}
	files := http.FileServer(http.FS(modules))
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		// The page is rebuilt with the backend, so nothing may keep an old
		// copy. The modules may be cached for a few minutes.
		path := strings.TrimPrefix(request.URL.Path, "/")
		if path == "" || path == "index.html" {
			response.Header().Set("Cache-Control", "no-store")
			response.Header().Set("Content-Type", "text/html; charset=utf-8")
			http.ServeContent(response, request, "index.html", time.Time{}, strings.NewReader(page))
			return
		}
		response.Header().Set("Cache-Control", staticAssetCacheControl)
		files.ServeHTTP(response, request)
	}), nil
}

// staticAssetCacheControl caches the scripts and styles beside the lobby page
// for five minutes, as the lobby page caches its own, so a deploy reaches
// players within five minutes without fingerprinted file names.
const staticAssetCacheControl = "public, max-age=300"
