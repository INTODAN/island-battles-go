package models

import "time"

// LobbyGameKey is the game key of Island Battles lobbies.
const LobbyGameKey = "island-battles"

// WatchState is what a watcher sees of a lobby's game: the board, the teams
// and the outcome. It has none of GenerationState's player-only fields, such
// as a player's team, bot, memory or submission.
type WatchState struct {
	GameID       uint           `json:"game_id" minimum:"1"`
	LobbyID      string         `json:"lobby_id" format:"uuid"`
	LobbyRound   int64          `json:"lobby_round" minimum:"1" doc:"The lobby round that the game plays. A lobby plays one game per round, and the /game/from-lobby/{lobby_id} routes serve its newest game: compare this with the lobby snapshot's lobby.round to tell whether the game is the one the lobby started last."`
	GenerationID uint           `json:"generation_id" minimum:"1"`
	Channel      string         `json:"channel" doc:"The lobby's watch channel, watch:island-battles:<lobby_id>, which receives WatchGenerationEvent after each turn and WatchGameEndedEvent after the last one."`
	Teams        []GameTeam     `json:"teams" nullable:"false" doc:"The teams that play, with their lobby names and the display names their players had at the start."`
	Generation   GenerationView `json:"generation"`
	// Outcome is nil, and left out, while the game runs.
	Outcome *GameOutcomeView `json:"outcome,omitempty" doc:"How the game ended. Absent while the game runs."`
	// TurnDeadlineAt is nil when the lobby set no turn timeout, and once the
	// game has ended.
	TurnDeadlineAt *time.Time `json:"turn_deadline_at" nullable:"true" doc:"When this generation's turn times out, as in GenerationState. Null when turns have no time limit, and once the game has ended. Watchers never apply a deadline themselves."`
}

// WatchGenerationEvent is published on the lobby's watch channel after a turn
// that did not end the game, in the same push as each player's
// GenerationEvent.
type WatchGenerationEvent struct {
	SchemaVersion int        `json:"schema_version" enum:"1"`
	Type          string     `json:"type" enum:"game.next_generation"`
	State         WatchState `json:"state"`
}

// WatchGameEndedEvent replaces WatchGenerationEvent for the turn that ends
// the game, and is the watch channel's last game event.
type WatchGameEndedEvent struct {
	SchemaVersion int        `json:"schema_version" enum:"1"`
	Type          string     `json:"type" enum:"game.ended"`
	State         WatchState `json:"state" doc:"The final generation, with its outcome."`
}
