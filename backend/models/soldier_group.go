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

	GenerationID          uint
	LogicalSoldierGroupID uint
	TeamColor             uint
	ClashRadius           float64      `json:"clash_radius" gorm:"not null;default:0.25;check:clash_radius > 0"`
	Position              pgtype.Point `gorm:"type:point"`
}

const SoldierGroupSpeedPerGeneration = 0.5
const DefaultClashRadius = 0.25

// EffectiveClashRadius also supplies the default for groups not yet persisted.
func (group SoldierGroup) EffectiveClashRadius() float64 {
	if group.ClashRadius == 0 {
		return DefaultClashRadius
	}
	return group.ClashRadius
}
