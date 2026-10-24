package lobbyadapter

import "islandBattles/lobby/frontend"

// FrontendGame lists Island Battles in the lobby frontend's game catalog. The
// result URL is the lobby replay route, and the demo package serves the
// example player module under /game/demo/.
func FrontendGame() frontend.Game {
	return frontend.Game{
		Key:         "island-battles",
		Name:        "Island Battles",
		ResultURL:   "/game/from-lobby/" + frontend.LobbyIDPlaceholder + "/replay",
		StartedHint: "Island Battles waits for player commands through the Python client. The board follows recorded generations live.",
		ExamplePlayer: &frontend.ExamplePlayer{
			Module:   "/game/demo/example-player.mjs",
			Strategy: "One soldier",
			Summary:  "Each player sends one soldier to the first enemy castle, then finishes the turn.",
		},
	}
}
