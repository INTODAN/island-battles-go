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
}
