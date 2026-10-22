package models

import "gorm.io/gorm"

type Game struct {
	gorm.Model
	LobbyID *string `gorm:"type:uuid;uniqueIndex"`
	Replay  []byte  `gorm:"type:jsonb" json:"-"`
	// NextSoldierGroupID is never decremented, so a departed group's ID is not reassigned.
	NextSoldierGroupID uint `gorm:"not null" json:"-"`
	Generations        []Generation
}
