package models

import (
	"github.com/jackc/pgx/v5/pgtype"
	"gorm.io/gorm"
)

type CastleFields struct {
	TeamColor int
	Position  pgtype.Point `gorm:"type:point" json:"-"`
	CastleId  uint         `json:"-"`
}

type Castle struct {
	gorm.Model
	CastleFields
	SoldierAmount int

	MapID uint
	Map   Map
}

type CastleTemplate struct {
	gorm.Model
	CastleFields
	MapTemplateID uint
	MapTemplate   MapTemplate
}
