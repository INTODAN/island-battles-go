package models

import (
	"github.com/jackc/pgx/v5/pgtype"
	"gorm.io/gorm"
)

type CastleFields struct {
	TeamColor          uint
	Position           pgtype.Point `gorm:"type:point"`
	PersistentCastleID uint         `json:"-"`
	Upgrades           Upgrades     `json:"upgrades,omitempty" gorm:"serializer:json;type:jsonb;not null;default:'{\"speed\":1,\"clash_stop_generations\":1}'"`
}

func (fields CastleFields) Clone() CastleFields {
	fields.Upgrades = fields.Upgrades.Clone()
	return fields
}

type Castle struct {
	gorm.Model
	CastleFields
	SoldierAmount uint

	MapID uint
	Map   Map
}

type CastleTemplate struct {
	gorm.Model
	CastleFields
	MapTemplateID uint
	MapTemplate   MapTemplate
}
