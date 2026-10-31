package lobbyadapter

import "islandBattles/lobby/frontend"

// FrontendGame lists Island Battles in the lobby frontend's game catalog. The
// result URL is the lobby replay route, the data URL prefix admits the lobby
// game's reads such as its logs, and the demo package serves the example
// player and the map choice settings modules under /game/demo/.
func FrontendGame() frontend.Game {
	return frontend.Game{
		Key:         "island-battles",
		Name:        "Island Battles",
		ResultURL:   "/game/from-lobby/" + frontend.LobbyIDPlaceholder + "/replay",
		StartedHint: "Island Battles waits for player commands through the Python client. The board follows recorded generations live.",
		// The replay reads each generation's game log through the page.
		DataURLPrefix: "/game/from-lobby/" + frontend.LobbyIDPlaceholder + "/",
		// The host picks the generated map, a default map or a map file, and
		// the map sets the lobby's teams.
		SettingsModule: "/game/demo/lobby-settings.mjs",
		ExamplePlayer: &frontend.ExamplePlayer{
			Module:   "/game/demo/example-player.mjs",
			Strategy: "One soldier",
			Summary:  "Each player sends one soldier to the first enemy castle, then finishes the turn.",
		},
	}
}
