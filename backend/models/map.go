package models

import (
	"github.com/restayway/gogis"
	"gorm.io/gorm"
)

type Map struct {
	gorm.Model
	MapName      string `json:"map_name" gorm:"primaryKey"`
	PlayerAmount int    `json:"player_amount"`
	PlaneSize    gogis.Point `json:"plane_size" gorm:"type:geometry(Point,4326)"`
}
