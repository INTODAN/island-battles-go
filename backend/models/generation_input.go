package models

import (
	"encoding/json"
	"time"
)

// FrozenGamePlayer fixes both turn participation and authority at game start.
type FrozenGamePlayer struct {
	GameID    uint   `gorm:"primaryKey;autoIncrement:false"`
	PlayerID  string `gorm:"type:uuid;primaryKey"`
	TeamColor uint   `gorm:"not null"`
	CreatedAt time.Time
}

type GenerationInput struct {
	GenerationID uint   `gorm:"primaryKey;autoIncrement:false"`
	PlayerID     string `gorm:"type:uuid;primaryKey"`
	Commands     []byte `gorm:"type:jsonb;not null"`
	Completed    bool   `gorm:"not null"`
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// Request IDs cannot be reused for another generation by the same game/player.
type GenerationRequest struct {
	GameID       uint   `gorm:"primaryKey;autoIncrement:false"`
	PlayerID     string `gorm:"type:uuid;primaryKey"`
	RequestID    string `gorm:"type:uuid;primaryKey"`
	GenerationID uint   `gorm:"not null"`
	PayloadHash  string `gorm:"not null"`
	CreatedAt    time.Time
}

// PublicGenerationCommand keeps raw parameters so the service can decode them
// strictly and reject other command types with 403. The published schema
// describes them as SendSoldierGroupParameters.
type PublicGenerationCommand struct {
	CommandType CommandType     `json:"command_type"`
	Parameters  json.RawMessage `json:"parameters"`
}

// SendSoldierGroupParameters orders soldiers from an owned castle. Pointers let
// validation distinguish a missing ID from castle 0.
type SendSoldierGroupParameters struct {
	OriginCastleID      *uint `json:"origin_castle_id" nullable:"false" doc:"Castle your team owns."`
	DestinationCastleID *uint `json:"destination_castle_id" nullable:"false" doc:"Any other castle."`
	SoldierAmount       *uint `json:"soldier_amount" minimum:"1" nullable:"false" doc:"Soldiers to send; counts against soldiers already reserved by your team this turn."`
}

type RegisterGenerationCommands struct {
	GameID       uint                      `json:"game_id" minimum:"1"`
	GenerationID uint                      `json:"generation_id" minimum:"1"`
	RequestID    string                    `json:"request_id" format:"uuid" doc:"Identifies this batch. Retry a lost request with the same UUID and body; use a new UUID for every new batch."`
	Complete     *bool                     `json:"complete,omitempty" doc:"Seals your turn after this batch. Defaults to true."`
	Commands     []PublicGenerationCommand `json:"commands" maxItems:"1000" nullable:"false"`
}

type GenerationState struct {
	GameID       uint           `json:"game_id" minimum:"1"`
	LobbyID      string         `json:"lobby_id" format:"uuid"`
	GenerationID uint           `json:"generation_id" minimum:"1"`
	PlayerID     string         `json:"player_id" format:"uuid"`
	TeamColor    uint           `json:"team_color" minimum:"1"`
	Channel      string         `json:"channel" doc:"Your personal Centrifugo channel, which receives GenerationEvent after each turn and GameEndedEvent after the last one."`
	Submitted    bool           `json:"submitted" doc:"Whether you completed this generation's turn."`
	Generation   GenerationView `json:"generation"`
	// Outcome is nil, and left out, while the game runs.
	Outcome *GameOutcomeView `json:"outcome,omitempty" doc:"How the game ended. Absent while the game runs. A finished game keeps its final generation and rejects every command."`
}

// GameOutcomeView is how a finished game ended.
type GameOutcomeView struct {
	Reason          GameOutcomeReason `json:"reason" enum:"last_team_standing,all_eliminated,generation_cap" doc:"last_team_standing: one team was left after every other team lost all its castles and soldier groups. all_eliminated: the last teams were eliminated in the same turn, a draw. generation_cap: the game reached its last generation; the team with the most castles won, and equal counts are a draw."`
	WinnerTeamColor *uint             `json:"winner_team_color" minimum:"1" nullable:"true" doc:"The winning team, or null for a draw."`
	Summary         string            `json:"summary" doc:"The outcome in one sentence, naming the winning team."`
}

const (
	GenerationEventSchemaVersion = 1
	GenerationEventType          = "game.next_generation"
	GameEndedEventType           = "game.ended"
)

// GenerationEvent is published on the player's personal channel after a turn
// that did not end the game.
type GenerationEvent struct {
	SchemaVersion int             `json:"schema_version" enum:"1"`
	Type          string          `json:"type" enum:"game.next_generation"`
	State         GenerationState `json:"state"`
}

// GameEndedEvent replaces GenerationEvent for the turn that ends the game, and
// is the game's last event.
type GameEndedEvent struct {
	SchemaVersion int             `json:"schema_version" enum:"1"`
	Type          string          `json:"type" enum:"game.ended"`
	State         GenerationState `json:"state" doc:"The final generation, with its outcome."`
}

// API views are published without their View suffix, e.g. as Castle.
type GenerationView struct {
	ID            uint               `json:"id" minimum:"1" doc:"Generation ID; increases with every turn."`
	Castles       []CastleView       `json:"castles" nullable:"false"`
	SoldierGroups []SoldierGroupView `json:"soldier_groups" nullable:"false"`
	Clashes       []Clash            `json:"clashes" nullable:"false"`
}

type CastleView struct {
	ID        uint    `json:"id" doc:"Castle ID, stable for the whole game."`
	TeamColor uint    `json:"team_color" doc:"Owning team; 0 is neutral."`
	Soldiers  uint    `json:"soldiers"`
	X         float64 `json:"x"`
	Y         float64 `json:"y"`
}

type SoldierGroupView struct {
	ID                  uint    `json:"id" doc:"Soldier group ID, never reassigned within a game."`
	TeamColor           uint    `json:"team_color" doc:"Owning team; 0 is neutral."`
	Soldiers            uint    `json:"soldiers"`
	X                   float64 `json:"x"`
	Y                   float64 `json:"y"`
	OriginCastleID      uint    `json:"origin_castle_id"`
	DestinationCastleID uint    `json:"destination_castle_id"`
	ClashRadius         float64 `json:"clash_radius" exclusiveMinimum:"0" doc:"Enemies clash when their centers are within the larger of their radii."`
}
