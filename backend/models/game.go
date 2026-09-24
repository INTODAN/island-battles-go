package models

import "gorm.io/gorm"

type Game struct {
	gorm.Model
	LobbyID     *string `gorm:"type:uuid;uniqueIndex"`
	Replay      []byte  `gorm:"type:jsonb" json:"-"`
	Generations []Generation
}
