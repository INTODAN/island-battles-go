package models

import "gorm.io/gorm"

type Game struct {
	gorm.Model
	LobbyID     *string `gorm:"type:uuid;uniqueIndex"`
	Generations []Generation
}
