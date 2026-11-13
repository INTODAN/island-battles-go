// Package mapbuilder serves the Island Battles map builder, a browser page
// that edits a map file and downloads it once POST /map/validate accepts it.
package mapbuilder

import (
	"bytes"
	"embed"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"islandBattles/backend/models"
)

//go:embed index.html app.mjs builder.mjs style.css
var assets embed.FS

// upgradesPath is the file, relative to the page, that lists the upgrade
// types a castle can start with, so the page offers exactly the levels the
// game accepts. It holds models.UpgradeTable, which GET /game/upgrades also
// serves; the page reads each type and max_level.
const upgradesPath = "upgrades.json"

// Handler serves the page. Mount it with its prefix stripped, on the same
// origin as POST /map/validate.
func Handler() (http.Handler, error) {
	upgrades, err := json.Marshal(models.UpgradeTable())
	if err != nil {
		return nil, err
	}
	files := http.FileServer(http.FS(assets))
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		// The page and the upgrade list are rebuilt with the backend, so nothing
		// may keep an old copy. The scripts and the style may be cached for a
		// few minutes.
		name := strings.TrimPrefix(request.URL.Path, "/")
		if name == "" || name == "index.html" || name == upgradesPath {
			response.Header().Set("Cache-Control", "no-store")
		} else {
			response.Header().Set("Cache-Control", staticAssetCacheControl)
		}
		if name == upgradesPath {
			response.Header().Set("Content-Type", "application/json")
			http.ServeContent(response, request, upgradesPath, time.Time{}, bytes.NewReader(upgrades))
			return
		}
		files.ServeHTTP(response, request)
	}), nil
}

// staticAssetCacheControl caches the scripts and styles beside the lobby page
// for five minutes, as the lobby page caches its own, so a deploy reaches
// players within five minutes without fingerprinted file names.
const staticAssetCacheControl = "public, max-age=300"
