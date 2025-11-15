package models

import (
	"gorm.io/gorm"
)

type MapFields struct {
	MapName      string `json:"map_name" gorm:"unique"`
	PlayerAmount int    `json:"player_amount"`
	PlaneSizeY    int `json:"plane_size_y"`
	PlaneSizeX    int `json:"plane_size_x"`
	NeutralStartingSoldierAmount int `json:"neutral_starting_soldier_amount"`
	Castles []Castle `json:"castles"`
}

type Map struct {
	gorm.Model
	MapFields
}

type MapTemplateFields struct {
	MapFields
	Castles []CastleTemplate `json:"castles"`
}

type MapTemplate struct {
	gorm.Model
	MapTemplateFields
}
