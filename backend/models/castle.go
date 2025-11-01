package models

import (
	"github.com/jackc/pgx/v5/pgtype"
	"gorm.io/gorm"
)

type CastleFields struct {
	TeamColor int `json:"team_color"`
	Position pgtype.Point `json:"position" gorm:"type:point"`
	SoldierAmount int `json:"soldier_amount"`
}

type Castle struct {
	gorm.Model
	CastleFields
}
