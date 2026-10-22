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

	GenerationID             uint
	PersistentSoldierGroupID uint
	TeamColor                uint
	MovementPauseGenerations uint         `json:"movement_pause_generations" gorm:"not null"`
	ClashRadius              float64      `json:"clash_radius" gorm:"not null;check:clash_radius > 0"`
	Position                 pgtype.Point `gorm:"type:point"`
	Upgrades                 Upgrades     `json:"upgrades" gorm:"serializer:json;type:jsonb;not null"`
}

const DefaultClashRadius = 0.25

// EffectiveClashRadius supplies the default for groups created without a radius.
func (group SoldierGroup) EffectiveClashRadius() float64 {
	if group.ClashRadius == 0 {
		return DefaultClashRadius
	}
	return group.ClashRadius
}
