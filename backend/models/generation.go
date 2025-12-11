package models

import (
	"gorm.io/gorm"
)

type Generation struct {
	gorm.Model
	MapID uint
	Map   Map
	// TODO: soldierGroups []SoldierGroup
}
