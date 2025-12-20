package models

import "gorm.io/gorm"

type Game struct {
	gorm.Model
	Generations []Generation
	Players     []Player
}

type Player struct {
	gorm.Model
	GameID    uint
	TeamColor int
}
