package models

import (
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

type CommandFields struct {
	CommandType CommandType
	Parameters  datatypes.JSON `gorm:"type:jsonb"`
}

type Command struct {
	gorm.Model
	CommandFields
	GenerationID uint `gorm:"index"`
}

type CommandType string

const (
	UpgradeCastle              CommandType = "upgrade_castle"
	IncreaseSoldierAmount      CommandType = "increase_soldier_amount"
	MoveSoldierGroups          CommandType = "move_soldier_groups"
	SendSoldierGroup           CommandType = "send_soldier_group"
	RemoveArrivedSoldierGroups CommandType = "remove_arrived_soldier_groups"
)
