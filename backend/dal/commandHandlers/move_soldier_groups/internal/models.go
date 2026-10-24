package internal

import (
	"islandBattles/backend/models"

	"github.com/jackc/pgx/v5/pgtype"
)

type SoldierGroupMoveDelta struct {
	Start        pgtype.Point
	End          pgtype.Point
	SoldierGroup *models.SoldierGroup
	// Nil means the destination is beyond this turn; otherwise this is the
	// fraction of the turn at which the group leaves the field.
	ArrivalTime      *float64
	StartTime        float64
	PauseGenerations uint
}

type ClashParticipant struct {
	SoldierGroup *models.SoldierGroup
	Position     pgtype.Point
	// SoldiersBefore is the group's count at contact. Nothing changes it later.
	SoldiersBefore uint
	// SoldiersAfter is the group's count once the clash is resolved. Only
	// resolving the clash sets it.
	SoldiersAfter uint
}

// Clash groups simultaneous contacts, retaining each group's own position.
type Clash struct {
	Time         float64
	Participants []ClashParticipant
}
