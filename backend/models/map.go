package models

import (
	"gorm.io/gorm"
)

type MapFields struct {
	MapName                      string `json:"map_name"`
	PlayerAmount                 uint   `json:"player_amount" default:"2"`
	PlaneSizeY                   uint   `json:"plane_size_y"`
	PlaneSizeX                   uint   `json:"plane_size_x"`
	NeutralStartingSoldierAmount uint   `json:"neutral_starting_soldier_amount" default:"15"`
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
