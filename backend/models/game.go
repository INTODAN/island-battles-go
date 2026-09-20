package models

import "gorm.io/gorm"

type Game struct {
	gorm.Model
	// LobbyID is assigned only by the explicit started-lobby binding operation.
	// Fixture and legacy games remain detached with a SQL NULL lobby_id.
	LobbyID     *string `gorm:"type:uuid;uniqueIndex"`
	Generations []Generation
}
