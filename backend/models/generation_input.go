package models

import (
	"encoding/json"
	"reflect"
	"time"

	"github.com/danielgtaylor/huma/v2"
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

type GenerationNotification struct {
	ID           uint   `gorm:"primaryKey"`
	GameID       uint   `gorm:"not null"`
	GenerationID uint   `gorm:"not null;uniqueIndex:ux_generation_notification_player"`
	PlayerID     string `gorm:"type:uuid;not null;uniqueIndex:ux_generation_notification_player"`
	Payload      []byte `gorm:"type:jsonb;not null"`
	PublishedAt  *time.Time
	CreatedAt    time.Time
}

type PublicGenerationCommand struct {
	CommandType CommandType     `json:"command_type" enum:"send_soldier_group"`
	Parameters  json.RawMessage `json:"parameters"`
}

// SendSoldierGroupParameters describes the only public player command. IDs are
// persistent IDs, so zero is valid and must not be confused with omission.
type SendSoldierGroupParameters struct {
	OriginPersistentCastleID      uint `json:"origin_persistent_castle_id" minimum:"0"`
	DestinationPersistentCastleID uint `json:"destination_persistent_castle_id" minimum:"0"`
	SoldierAmount                 uint `json:"soldier_amount" minimum:"1"`
}

// TransformSchema gives HTTP validation and generators the typed public payload.
// The service retains raw JSON to reject malformed direct (non-HTTP) callers and
// hash the submitted payload for idempotency before converting internal commands.
func (PublicGenerationCommand) TransformSchema(registry huma.Registry, schema *huma.Schema) *huma.Schema {
	schema.Properties["parameters"] = registry.Schema(reflect.TypeOf(SendSoldierGroupParameters{}), true, "SendSoldierGroupParameters")
	return schema
}

// GameNextGeneration is the versioned personal-channel event. Its state uses
// exactly the same schema and representation as the authoritative HTTP response.
type GameNextGeneration struct {
	SchemaVersion int             `json:"schema_version" enum:"1"`
	Type          string          `json:"type" enum:"game.next_generation"`
	State         GenerationState `json:"state"`
}

type RegisterGenerationCommands struct {
	GameID       uint                      `json:"game_id" minimum:"1"`
	GenerationID uint                      `json:"generation_id" minimum:"1"`
	RequestID    string                    `json:"request_id" format:"uuid"`
	Complete     *bool                     `json:"complete,omitempty"`
	Commands     []PublicGenerationCommand `json:"commands" maxItems:"1000" nullable:"false"`
}

type GenerationState struct {
	GameID       uint           `json:"game_id" minimum:"1"`
	LobbyID      string         `json:"lobby_id" format:"uuid"`
	GenerationID uint           `json:"generation_id" minimum:"1"`
	PlayerID     string         `json:"player_id" format:"uuid"`
	TeamColor    uint           `json:"team_color" minimum:"1"`
	Channel      string         `json:"channel" pattern:"^player:[0-9a-fA-F-]{36}$"`
	Submitted    bool           `json:"submitted"`
	Generation   GenerationView `json:"generation"`
}

type GenerationView struct {
	Castles       []CastleView       `json:"castles" nullable:"false"`
	SoldierGroups []SoldierGroupView `json:"soldier_groups" nullable:"false"`
	Clashes       []ClashEvent       `json:"clashes,omitempty" nullable:"false"`
}

type CastleView struct {
	ID        uint    `json:"id" minimum:"0"`
	TeamColor uint    `json:"team_color" minimum:"0"`
	Soldiers  uint    `json:"soldiers" minimum:"0"`
	X         float64 `json:"x"`
	Y         float64 `json:"y"`
}

type SoldierGroupView struct {
	ID                            uint    `json:"id" minimum:"0"`
	TeamColor                     uint    `json:"team_color" minimum:"0"`
	Soldiers                      uint    `json:"soldiers" minimum:"0"`
	X                             float64 `json:"x"`
	Y                             float64 `json:"y"`
	OriginPersistentCastleID      uint    `json:"origin_persistent_castle_id" minimum:"0"`
	DestinationPersistentCastleID uint    `json:"destination_persistent_castle_id" minimum:"0"`
	ClashRadius                   float64 `json:"clash_radius" minimum:"0"`
}
