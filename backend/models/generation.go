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
	Clashes       []ClashEvent `gorm:"serializer:json;type:jsonb"`
}
