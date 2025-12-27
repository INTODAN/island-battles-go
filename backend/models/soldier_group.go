package models

import (
	"github.com/jackc/pgx/v5/pgtype"
	"gorm.io/gorm"
)

type SoldierGroupFields struct {
	OriginCastleID uint
	OriginCastle   Castle

	DestinationCastleID uint
	DestinationCastle   Castle

	GenerationID uint

	SoldierAmount uint
	Position      pgtype.Point `gorm:"type:point"`
}

type SoldierGroup struct {
	gorm.Model
	SoldierGroupFields
}

const SoldierGroupSpeedPerGeneration = 0.5
