package lobbyadapter_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"islandBattles/backend/demo"
	"islandBattles/backend/demoviewer"
	"islandBattles/backend/lobbyadapter"
	"islandBattles/lobby/frontend"
	"islandBattles/lobby/gamehooks"
)

func TestFrontendGameIsAValidCatalogEntry(t *testing.T) {
	if _, err := frontend.Handler([]frontend.Game{catalogEntry(t)}); err != nil {
		t.Fatal(err)
	}
}

// The catalog's modules are served where main.go mounts the demo handler.
func TestFrontendGameModulesAreServed(t *testing.T) {
	game := catalogEntry(t)
	handler := http.StripPrefix("/game/demo/", demo.Handler())
	for _, module := range []string{game.ExamplePlayer.Module, game.SettingsModule, game.RoomModule} {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, module, nil))
		if recorder.Code != http.StatusOK || !strings.Contains(recorder.Header().Get("Content-Type"), "javascript") {
			t.Errorf("GET %s: status %d, content type %q", module, recorder.Code, recorder.Header().Get("Content-Type"))
		}
	}
}

// The room module reaches the lobby's bot packages, and nothing of the game
// that a roster player reads.
func TestFrontendGameRoomReachesTheBotPackages(t *testing.T) {
	prefix := catalogEntry(t).RoomAPIPrefix
	if prefix != "/game/lobbies/{lobby_id}/" || !strings.HasPrefix("/game/lobbies/{lobby_id}/bot-packages", prefix) {
		t.Fatalf("room API prefix = %q", prefix)
	}
	if strings.HasPrefix("/game/from-lobby/{lobby_id}/generation", prefix) {
		t.Fatal("the room module reaches the game's reads")
	}
}

// The catalog links the demo viewer where app.go mounts it.
func TestFrontendGameLinksTheDemoViewer(t *testing.T) {
	links := catalogEntry(t).Links
	if len(links) != 2 || links[0].Label != "Watch a saved demo" {
		t.Fatalf("links = %+v", links)
	}
	viewer, err := demoviewer.Handler()
	if err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	http.StripPrefix("/game/demo-viewer/", viewer).ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, links[0].URL, nil))
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), "Open demo file") {
		t.Fatalf("GET %s: status %d", links[0].URL, recorder.Code)
	}
}

// A watcher's frame reads only the watcher routes: the catalog gives it a
// prefix of their own, which names none of a player's reads.
func TestFrontendGameWatchersReadOnlyTheWatchRoutes(t *testing.T) {
	game := catalogEntry(t)
	if game.WatchResultURL != "/game/from-lobby/{lobby_id}/watch/replay" || game.WatchDataURLPrefix != "/game/from-lobby/{lobby_id}/watch/" {
		t.Fatalf("watch URLs = %q, %q", game.WatchResultURL, game.WatchDataURLPrefix)
	}
	for _, playerRead := range []string{"/game/from-lobby/{lobby_id}/demo", "/game/from-lobby/{lobby_id}/logs", "/game/from-lobby/{lobby_id}/generation", "/game/from-lobby/{lobby_id}/replay"} {
		if strings.HasPrefix(playerRead, game.WatchDataURLPrefix) {
			t.Errorf("%s is under the watch prefix", playerRead)
		}
	}
	for _, watchRead := range []string{"/game/from-lobby/{lobby_id}/watch/demo", game.WatchResultURL} {
		if !strings.HasPrefix(watchRead, game.WatchDataURLPrefix) {
			t.Errorf("%s is not under the watch prefix", watchRead)
		}
	}
}

// The web client is served at /play/ by CloudFront, not the backend
// (deploy/README.md, "The web client").
func TestFrontendGameLinksThe3DClient(t *testing.T) {
	links := catalogEntry(t).Links
	if len(links) != 2 || links[1].Label != "Play in 3D" || links[1].URL != "/play/" {
		t.Fatalf("links = %+v", links)
	}
}

// For a lobby page on another origin, every URL of the entry is on the
// game's origin, which the lobby checks it against.
func TestFrontendGameOnTheGamesOwnOrigin(t *testing.T) {
	origin := "https://island-battles.example.test"
	entry, err := lobbyadapter.LobbyGame(origin+lobbyadapter.HooksPath, origin)
	if err != nil {
		t.Fatal(err)
	}
	games, err := gamehooks.ParseGames("["+string(entry)+"]", func(name string) (string, bool) {
		return string(hookSecret), name == lobbyadapter.SecretEnv
	})
	if err != nil {
		t.Fatal(err)
	}
	game, paths := games[0].Frontend, catalogEntry(t)
	if _, err := frontend.Handler([]frontend.Game{game}); err != nil {
		t.Fatal(err)
	}
	if game.Origin != origin || paths.Origin != "" {
		t.Fatalf("origins %q and %q", game.Origin, paths.Origin)
	}
	for _, pair := range [][2]string{
		{game.ResultURL, paths.ResultURL}, {game.DataURLPrefix, paths.DataURLPrefix}, {game.WatchResultURL, paths.WatchResultURL},
		{game.WatchDataURLPrefix, paths.WatchDataURLPrefix}, {game.SettingsModule, paths.SettingsModule}, {game.RoomModule, paths.RoomModule},
		{game.RoomAPIPrefix, paths.RoomAPIPrefix}, {game.ExamplePlayer.Module, paths.ExamplePlayer.Module},
		{game.Links[0].URL, paths.Links[0].URL}, {game.Links[1].URL, paths.Links[1].URL},
	} {
		if pair[0] != origin+pair[1] {
			t.Errorf("%q is not %q on the game's origin", pair[0], pair[1])
		}
	}
	// The lobby refuses the entry when its URLs are on another origin.
	game.Origin = "https://other.example.test"
	if _, err := frontend.Handler([]frontend.Game{game}); err == nil {
		t.Fatal("a catalog on another origin was accepted")
	}
}

// catalogEntry is the lobby page's catalog entry of Island Battles, from
// LobbyGame as the lobby reads it.
func catalogEntry(t *testing.T) frontend.Game {
	t.Helper()
	return configuredGames(t)[0].Frontend
}
