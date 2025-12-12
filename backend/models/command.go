package models

import (
	"github.com/jackc/pgx/v5/pgtype"
	"gorm.io/gorm"
)

type CommandFields struct {
	GenerationId uint
	CommandType  CommandType
	Parameters   pgtype.JSONBCodec `gorm:"type:jsonb"`
}

type Command struct {
	gorm.Model
	CommandFields
}

type CommandType string

const (
	SoldierAmountIncrease CommandType = "SOLDIER_AMOUNT_INCREASE"
)

var CommandParams = map[CommandType]interface{}{
	SoldierAmountIncrease: struct{}{},
}
