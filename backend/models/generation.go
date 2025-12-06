package models

import (
	"gorm.io/gorm"
)

type Generation struct {
	gorm.Model
	Map Map
	// TODO: soldierGroups []SoldierGroup
}
 