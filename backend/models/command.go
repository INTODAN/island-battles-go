package models

import (
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

type CommandFields struct {
	GenerationId uint
	CommandType  CommandType
	Parameters   datatypes.JSON `gorm:"type:jsonb"`
}

type Command struct {
	gorm.Model
	CommandFields
}

type CommandType string

const (
	IncreaseSoldierAmount CommandType = "increase_soldier_amount"
)
