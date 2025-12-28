package models

import (
	"github.com/jackc/pgx/v5/pgtype"
	"gorm.io/gorm"
)

type SoldierGroupFields struct {
	OriginCastleID      uint
	DestinationCastleID uint
	SoldierAmount       uint
}

type SoldierGroup struct {
	gorm.Model
	SoldierGroupFields

	OriginCastle      Castle `json:"-"`
	DestinationCastle Castle `json:"-"`

	GenerationID uint
	Position     pgtype.Point `gorm:"type:point"`
}

const SoldierGroupSpeedPerGeneration = 0.5
