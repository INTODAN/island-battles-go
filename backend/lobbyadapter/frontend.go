package lobbyadapter

import "islandBattles/lobby/frontend"

// FrontendGame lists Island Battles in the lobby frontend's game catalog. The
// result URL is the lobby replay route of the lobby's round, the data URL prefix admits the lobby
// game's reads such as its logs and its demo, the demo package serves the
// example player, the map choice settings module and the bot choice room
// module under /game/demo/, and the link opens the demo viewer under
// /game/demo-viewer/. The room module calls the lobby's bot package
// endpoints under its room API prefix.
func FrontendGame() frontend.Game {
	return frontend.Game{
		Key:         "island-battles",
		Name:        "Island Battles",
		ResultURL:   "/game/from-lobby/" + frontend.LobbyIDPlaceholder + "/replay?lobby_round=" + frontend.RoundPlaceholder,
		StartedHint: "A default bot is played by the backend; a localhost bot is played by its player, whose open lobby tab relays each turn to the bot on their machine. The board follows recorded generations live.",
		// The replay reads each generation's game log and saves the demo
		// through the page.
		DataURLPrefix: "/game/from-lobby/" + frontend.LobbyIDPlaceholder + "/",
		// A watcher outside the roster sees the replay of the game through the
		// watcher routes, and its frame may read only those.
		WatchResultURL:     "/game/from-lobby/" + frontend.LobbyIDPlaceholder + "/watch/replay",
		WatchDataURLPrefix: "/game/from-lobby/" + frontend.LobbyIDPlaceholder + "/watch/",
		// The host picks the generated map, a default map or a map file, and
		// the map sets the lobby's teams.
		SettingsModule: "/game/demo/lobby-settings.mjs",
		// Every member chooses a bot in the waiting room before getting
		// ready, in their lobby member settings, which the page saves. The
		// room module also uploads, lists and deletes the lobby's bot
		// packages, under /game/lobbies/{lobby_id}/bot-packages.
		RoomModule:    "/game/demo/bot-choice.mjs",
		RoomAPIPrefix: "/game/lobbies/" + frontend.LobbyIDPlaceholder + "/",
		// A saved demo opens without the lobby, so the viewer stays
		// reachable after it closes. The 3D web client is served at /play/ by
		// the deployment (deploy/README.md, "The web client"), not by the backend.
		Links: []frontend.Link{
			{Label: "Watch a saved demo", URL: "/game/demo-viewer/"},
			{Label: "Play in 3D", URL: "/play/"},
		},
		ExamplePlayer: &frontend.ExamplePlayer{
			Module:   "/game/demo/example-player.mjs",
			Strategy: "One soldier",
			Summary:  "Each player sends one soldier to the first enemy castle, then finishes the turn.",
		},
	}
}
