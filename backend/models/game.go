package models

import "gorm.io/gorm"

type Game struct {
	gorm.Model
	RoomName    string `gorm:"unique"`
	Generations []Generation
	Players     []Player
}

type Player struct {
	gorm.Model
	GameID    uint
	TeamColor int
}
