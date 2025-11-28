package models

import (
	"gorm.io/gorm"
)

type MapFields struct {
	MapName      string `json:"map_name" gorm:"unique"`
	PlayerAmount int    `json:"player_amount" default:"2"`
	PlaneSizeY    int `json:"plane_size_y"`
	PlaneSizeX    int `json:"plane_size_x"`
	NeutralStartingSoldierAmount int `json:"neutral_starting_soldier_amount" default:"15"`
}

type Map struct {
	gorm.Model
	MapFields
	Castles []Castle `json:"castles"`
}

type MapTemplate struct {
	gorm.Model
	MapFields
	CastleTemplates []CastleTemplate `json:"castles"`
}
