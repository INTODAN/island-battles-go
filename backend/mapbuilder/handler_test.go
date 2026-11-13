package mapbuilder_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"islandBattles/backend/mapbuilder"
	"islandBattles/backend/models"
)

func get(t *testing.T, handler http.Handler, path string) *http.Response {
	t.Helper()
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
	return recorder.Result()
}

func TestHandlerServesThePage(t *testing.T) {
	handler, err := mapbuilder.Handler(nil)
	if err != nil {
		t.Fatal(err)
	}
	for path, contentType := range map[string]string{"/": "text/html", "/app.mjs": "text/javascript", "/builder.mjs": "text/javascript", "/style.css": "text/css"} {
		response := get(t, handler, path)
		if response.StatusCode != http.StatusOK || !strings.HasPrefix(response.Header.Get("Content-Type"), contentType) {
			t.Fatalf("%s: status=%d content type=%q", path, response.StatusCode, response.Header.Get("Content-Type"))
		}
	}
	page, _ := io.ReadAll(get(t, handler, "/").Body)
	for _, asset := range []string{`src="./app.mjs"`, `href="./style.css"`} {
		if !strings.Contains(string(page), asset) {
			t.Fatalf("the page does not load %s", asset)
		}
	}
	if response := get(t, handler, "/handler.go"); response.StatusCode != http.StatusNotFound {
		t.Fatalf("source file served: status=%d", response.StatusCode)
	}
}

func TestHandlerListsTheGamesUpgrades(t *testing.T) {
	handler, err := mapbuilder.Handler(nil)
	if err != nil {
		t.Fatal(err)
	}
	response := get(t, handler, "/upgrades.json")
	if response.StatusCode != http.StatusOK || response.Header.Get("Content-Type") != "application/json" {
		t.Fatalf("status=%d content type=%q", response.StatusCode, response.Header.Get("Content-Type"))
	}
	var upgrades []models.UpgradeView
	if err := json.NewDecoder(response.Body).Decode(&upgrades); err != nil {
		t.Fatal(err)
	}
	listed := map[models.UpgradeType]int{}
	for _, upgrade := range upgrades {
		listed[upgrade.Type] = upgrade.MaxLevel
		// Every listed level is one a map file may use, and the next is not.
		if err := (models.Upgrades{upgrade.Type: upgrade.MaxLevel}).Validate(); err != nil {
			t.Fatalf("%s level %d rejected: %v", upgrade.Type, upgrade.MaxLevel, err)
		}
		if err := (models.Upgrades{upgrade.Type: upgrade.MaxLevel + 1}).Validate(); err == nil {
			t.Fatalf("%s accepts level %d above the listed maximum", upgrade.Type, upgrade.MaxLevel+1)
		}
	}
	expected := map[models.UpgradeType]int{models.SpeedUpgrade: 4, models.ClashStopGenerationsUpgrade: 3}
	if !reflect.DeepEqual(listed, expected) {
		t.Fatalf("upgrades=%v, want %v", listed, expected)
	}
	if upgrades[0].Type != models.ClashStopGenerationsUpgrade {
		t.Fatalf("upgrades are not sorted by type: %v", upgrades)
	}
}

func TestHandlerCachesOnlyTheScriptsAndTheStyle(t *testing.T) {
	handler, err := mapbuilder.Handler(nil)
	if err != nil {
		t.Fatal(err)
	}
	for path, want := range map[string]string{
		"/": "no-store", "/index.html": "no-store", "/upgrades.json": "no-store",
		"/app.mjs": "public, max-age=300", "/builder.mjs": "public, max-age=300", "/style.css": "public, max-age=300",
		// Go's file server drops Cache-Control from an error answer, so a missing
		// file's 404 is never kept.
		"/missing.mjs": "",
	} {
		if got := get(t, handler, path).Header.Get("Cache-Control"); got != want {
			t.Errorf("%s: Cache-Control %q, want %q", path, got, want)
		}
	}
}

func TestHandlerListsTheLobbyOriginsThatMayTakeItsMap(t *testing.T) {
	for _, test := range []struct {
		origins []string
		want    string
	}{{nil, "[]"}, {[]string{"https://lobby.example.org", "http://localhost:8890"}, `["https://lobby.example.org","http://localhost:8890"]`}} {
		handler, err := mapbuilder.Handler(test.origins)
		if err != nil {
			t.Fatal(err)
		}
		response := get(t, handler, "/lobby-origins.json")
		body, _ := io.ReadAll(response.Body)
		if response.StatusCode != http.StatusOK || response.Header.Get("Content-Type") != "application/json" ||
			response.Header.Get("Cache-Control") != "no-store" || string(body) != test.want {
			t.Fatalf("status=%d type=%q cache=%q body=%s", response.StatusCode, response.Header.Get("Content-Type"), response.Header.Get("Cache-Control"), body)
		}
	}
}
