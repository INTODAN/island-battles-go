package models

import (
	"gorm.io/gorm"
)

// MapFields are shared by game maps and map templates, and are the top-level
// fields of the map file format. The plane is centred on the origin.
type MapFields struct {
	MapName                      string `json:"map_name" minLength:"1" doc:"Name of the map, unique among default maps."`
	PlayerAmount                 uint   `json:"player_amount" minimum:"2" maximum:"16" doc:"Teams the map is made for, from 2 to 16, one player each. Castle team colours run from 1 to player_amount, and every one of them owns at least one castle."`
	PlaneSizeY                   uint   `json:"plane_size_y" minimum:"1" doc:"Height of the plane, which is centred on the origin."`
	PlaneSizeX                   uint   `json:"plane_size_x" minimum:"1" doc:"Width of the plane, which is centred on the origin."`
	NeutralStartingSoldierAmount uint   `json:"neutral_starting_soldier_amount" doc:"Soldiers in every castle, owned or neutral, that sets no starting_soldier_amount."`
}

type Map struct {
	gorm.Model
	MapFields
	Castles []Castle `json:"castles"`
}

// MapTemplate stores a default map. Convert it to and from the map file
// format with View and NewMapTemplate.
type MapTemplate struct {
	gorm.Model
	MapFields
	CastleTemplates []CastleTemplate `json:"castles"`
}
