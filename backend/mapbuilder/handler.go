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

// lobbyOriginsPath is the file, relative to the page, that lists the lobby
// sites whose lobby page may open the builder and take its map, besides the
// builder's own origin: the game's ISLAND_BATTLES_LOBBY_ORIGINS.
const lobbyOriginsPath = "lobby-origins.json"

// Handler serves the page. Mount it with its prefix stripped, on the same
// origin as POST /map/validate. A lobby page on one of lobbyOrigins, as well
// as one on the builder's own origin, can take the builder's map.
func Handler(lobbyOrigins []string) (http.Handler, error) {
	upgrades, err := json.Marshal(models.UpgradeTable())
	if err != nil {
		return nil, err
	}
	if lobbyOrigins == nil {
		lobbyOrigins = []string{}
	}
	origins, err := json.Marshal(lobbyOrigins)
	if err != nil {
		return nil, err
	}
	files := http.FileServer(http.FS(assets))
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		// The page, the upgrade list and the lobby origins are rebuilt with the
		// backend, so nothing may keep an old copy. The scripts and the style may be cached for a
		// few minutes.
		name := strings.TrimPrefix(request.URL.Path, "/")
		if name == "" || name == "index.html" || name == upgradesPath || name == lobbyOriginsPath {
			response.Header().Set("Cache-Control", "no-store")
		} else {
			response.Header().Set("Cache-Control", staticAssetCacheControl)
		}
		if name == upgradesPath || name == lobbyOriginsPath {
			body := upgrades
			if name == lobbyOriginsPath {
				body = origins
			}
			response.Header().Set("Content-Type", "application/json")
			http.ServeContent(response, request, name, time.Time{}, bytes.NewReader(body))
			return
		}
		files.ServeHTTP(response, request)
	}), nil
}

// staticAssetCacheControl caches the scripts and styles beside the lobby page
// for five minutes, as the lobby page caches its own, so a deploy reaches
// players within five minutes without fingerprinted file names.
const staticAssetCacheControl = "public, max-age=300"
