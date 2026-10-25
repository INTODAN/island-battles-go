// Package mapbuilder serves the Island Battles map builder, a browser page
// that edits a map file and downloads it once POST /map/validate accepts it.
package mapbuilder

import (
	"bytes"
	"embed"
	"encoding/json"
	"maps"
	"net/http"
	"slices"
	"strings"
	"time"

	"islandBattles/backend/models"
)

//go:embed index.html app.mjs builder.mjs style.css
var assets embed.FS

// upgradesPath is the file, relative to the page, that lists the upgrade
// types a castle can start with, so the page offers exactly the levels the
// game accepts.
const upgradesPath = "upgrades.json"

// Upgrade is one upgrade type the page offers, from level 1 to MaxLevel.
type Upgrade struct {
	Type     models.UpgradeType `json:"type"`
	MaxLevel int                `json:"max_level"`
}

// Upgrades lists every upgrade type in a stable order.
func Upgrades() []Upgrade {
	maxLevels := models.UpgradeMaxLevels()
	upgrades := make([]Upgrade, 0, len(maxLevels))
	for _, upgradeType := range slices.Sorted(maps.Keys(maxLevels)) {
		upgrades = append(upgrades, Upgrade{Type: upgradeType, MaxLevel: maxLevels[upgradeType]})
	}
	return upgrades
}

// Handler serves the page. Mount it with its prefix stripped, on the same
// origin as POST /map/validate.
func Handler() (http.Handler, error) {
	upgrades, err := json.Marshal(Upgrades())
	if err != nil {
		return nil, err
	}
	files := http.FileServer(http.FS(assets))
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		// The page is rebuilt with the backend, so browsers must not keep an old copy.
		response.Header().Set("Cache-Control", "no-store")
		if strings.TrimPrefix(request.URL.Path, "/") == upgradesPath {
			response.Header().Set("Content-Type", "application/json")
			http.ServeContent(response, request, upgradesPath, time.Time{}, bytes.NewReader(upgrades))
			return
		}
		files.ServeHTTP(response, request)
	}), nil
}
