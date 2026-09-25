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
	CommandType CommandType     `json:"command_type"`
	Parameters  json.RawMessage `json:"parameters"`
}

type RegisterGenerationCommands struct {
	GameID       uint                      `json:"game_id" minimum:"1"`
	GenerationID uint                      `json:"generation_id" minimum:"1"`
	RequestID    string                    `json:"request_id" format:"uuid"`
	Complete     *bool                     `json:"complete,omitempty"`
	Commands     []PublicGenerationCommand `json:"commands" maxItems:"1000"`
}

type GenerationState struct {
	GameID       uint           `json:"game_id"`
	LobbyID      string         `json:"lobby_id"`
	GenerationID uint           `json:"generation_id"`
	PlayerID     string         `json:"player_id"`
	TeamColor    uint           `json:"team_color"`
	Channel      string         `json:"channel"`
	Submitted    bool           `json:"submitted"`
	Generation   GenerationView `json:"generation"`
}

type GenerationView struct {
	Castles       []CastleView       `json:"castles"`
	SoldierGroups []SoldierGroupView `json:"soldier_groups"`
}

type CastleView struct {
	ID        uint    `json:"id"`
	TeamColor uint    `json:"team_color"`
	Soldiers  uint    `json:"soldiers"`
	X         float64 `json:"x"`
	Y         float64 `json:"y"`
}

type SoldierGroupView struct {
	ID                         uint    `json:"id"`
	TeamColor                  uint    `json:"team_color"`
	Soldiers                   uint    `json:"soldiers"`
	X                          float64 `json:"x"`
	Y                          float64 `json:"y"`
	OriginLogicalCastleID      uint    `json:"origin_logical_castle_id"`
	DestinationLogicalCastleID uint    `json:"destination_logical_castle_id"`
	ClashRadius                float64 `json:"clash_radius"`
}
