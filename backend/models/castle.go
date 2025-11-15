package models

import (
	"github.com/jackc/pgx/v5/pgtype"
	"gorm.io/gorm"
)

type CastleFields struct {
	TeamColor int
	Position pgtype.Point `gorm:"type:point"`
	SoldierAmount int
}

type Castle struct {
	gorm.Model
	CastleFields
}
