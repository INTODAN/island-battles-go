package models

import "time"

// DemoFormat names the demo file format in every demo's format key.
const DemoFormat = "island-battles-demo"

// DemoSchemaVersion is the demo file format's version. Change it whenever the
// format, or a type it holds such as GenerationView, changes in a way that an
// older viewer can't read. The viewer opens only this version.
const DemoSchemaVersion = 2

// Demo is a saved game: every generation played so far, with every team's
// game logs. It is a file that the player keeps, so it holds
// everything the demo viewer shows, without a lobby, a session or the game.
type Demo struct {
	Format        string     `json:"format" enum:"island-battles-demo" doc:"Marks the file as an Island Battles demo."`
	SchemaVersion int        `json:"schema_version" enum:"2" doc:"The demo format's version. The demo viewer opens only the current version and rejects a demo saved by another version of the game."`
	SavedAt       time.Time  `json:"saved_at" doc:"When the demo was saved."`
	GameID        uint       `json:"game_id" minimum:"1"`
	Teams         []GameTeam `json:"teams" nullable:"false" doc:"The teams that played, with their lobby names and the display names their players had at the start."`
	// Huma can't mark a reference nullable, so routes.SetupGameRoutes
	// publishes outcome as a GameOutcome or null.
	Outcome *GameOutcomeView `json:"outcome" doc:"How the game ended, or null when the demo was saved while the game was still running."`
	Frames  []GenerationView `json:"frames" nullable:"false" minItems:"1" doc:"Every generation played when the demo was saved, in order. Each is the board view that players received."`
	Logs    []GenerationLogs `json:"logs" nullable:"false" doc:"One entry per frame, in the same order, with every team's game log lines when the demo was saved. A demo saved during the game lacks the lines that bots logged for the open turn after it was saved."`
}
