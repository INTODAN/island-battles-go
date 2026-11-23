package models

import (
	"time"

	"gorm.io/gorm"
)

type Generation struct {
	gorm.Model
	// The foreign key columns are indexed, so loading a game's generations
	// and deleting them with retention don't scan every game's rows.
	MapID         uint `gorm:"index"`
	GameID        uint `gorm:"index"`
	Map           Map
	Commands      []Command
	SoldierGroups []SoldierGroup
	Clashes       []Clash `gorm:"serializer:json;type:jsonb"`
	// TurnDeadlineAt is when the turn of a real-time game's generation ends,
	// for every generation but the first: the previous turn's end plus the
	// pace, so that turns come one pace apart whatever the latency of the
	// request that plays them (nextTurnDeadline in backend/services). It is
	// nil for a game's first generation, whose turn ends the pace after the
	// game's start, its creation time, and in a game that waits for every
	// player.
	TurnDeadlineAt *time.Time
	// Number is the generation's position in its game, from 1: its turn
	// number. It isn't stored: the DAL's loaders, GetGeneration,
	// GetLatestGenerationForGame, RefreshGeneration and LoadGameGenerations,
	// count it, and a generation loaded another way has 0.
	Number int `gorm:"-"`
}
