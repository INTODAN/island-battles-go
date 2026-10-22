package models

import (
	"gorm.io/gorm"
)

type Generation struct {
	gorm.Model
	MapID         uint
	GameID        uint
	Map           Map
	Commands      []Command
	SoldierGroups []SoldierGroup
	Clashes       []Clash `gorm:"serializer:json;type:jsonb"`
}
