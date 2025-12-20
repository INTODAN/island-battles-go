package models

import (
	"gorm.io/gorm"
)

type Generation struct {
	gorm.Model
	MapID         uint
	GameId        uint
	Map           Map
	SoldierGroups []SoldierGroup
}
