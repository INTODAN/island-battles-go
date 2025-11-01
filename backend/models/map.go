package models

import (
	"gorm.io/gorm"
)

type Map struct {
	gorm.Model
	MapName      string `json:"map_name" gorm:"unique"`
	PlayerAmount int    `json:"player_amount"`
	PlaneSizeY    int `json:"plane_size_y"`
	PlaneSizeX    int `json:"plane_size_x"`
	NeutralStartingSoldierAmount int `json:"neutral_starting_soldier_amount"`
}
