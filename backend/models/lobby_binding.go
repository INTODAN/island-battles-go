package models

import "time"

// TeamBindingRecord is game-specific persistence. Team IDs are generic lobby
// UUIDs; colors remain the Island Battles simulation's numeric identifiers.
type TeamBindingRecord struct {
	GameID    uint      `gorm:"column:game_id;primaryKey;uniqueIndex:ux_island_game_team_bindings_game_team;uniqueIndex:ux_island_game_team_bindings_game_color"`
	TeamID    string    `gorm:"column:team_id;type:uuid;primaryKey;uniqueIndex:ux_island_game_team_bindings_game_team"`
	TeamOrder int       `gorm:"column:team_order;not null"`
	TeamColor uint      `gorm:"column:team_color;not null;uniqueIndex:ux_island_game_team_bindings_game_color"`
	CreatedAt time.Time `gorm:"column:created_at;not null"`
}

func (TeamBindingRecord) TableName() string { return "island_game_team_bindings" }

type TeamBinding struct {
	TeamID    string `json:"team_id"`
	TeamOrder int    `json:"team_order"`
	TeamColor uint   `json:"team_color"`
}

type BindingResult struct {
	GameID  uint          `json:"game_id"`
	LobbyID string        `json:"lobby_id"`
	Teams   []TeamBinding `json:"teams"`
}
