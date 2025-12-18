package models

import "gorm.io/gorm"

type SoldierGroupFields struct {
	OriginCastleID uint
	OriginCastle   Castle

	DestinationCastleID uint
	DestinationCastle   Castle

	GenerationID uint

	SoldierAmount uint
}

type SoldierGroup struct {
	gorm.Model
	SoldierGroupFields
}
