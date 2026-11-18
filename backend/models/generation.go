package models

import (
	"gorm.io/gorm"
)

type Generation struct {
	gorm.Model
	// The foreign key columns are indexed, so loading a game's generations
	// and deleting them with retention don't scan every game's rows.
	MapID         uint `gorm:"index"`
	GameID        uint `gorm:"index"`
	Map           Map
	Commands      []Command
	SoldierGroups []SoldierGroup
	Clashes       []Clash `gorm:"serializer:json;type:jsonb"`
	// Number is the generation's position in its game, from 1: its turn
	// number. It isn't stored: the DAL's loaders, GetGeneration,
	// GetLatestGenerationForGame, RefreshGeneration and LoadGameGenerations,
	// count it, and a generation loaded another way has 0.
	Number int `gorm:"-"`
}
