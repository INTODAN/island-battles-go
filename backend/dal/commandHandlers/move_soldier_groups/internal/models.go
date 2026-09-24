package internal

import (
	"islandBattles/backend/models"

	"github.com/jackc/pgx/v5/pgtype"
)

type SoldierGroupMoveDelta struct {
	Start        pgtype.Point
	End          pgtype.Point
	SoldierGroup *models.SoldierGroup
}

type ClashParticipant struct {
	SoldierGroup *models.SoldierGroup
	Position     pgtype.Point
}

// Clash groups simultaneous contacts, retaining each group's own position.
type Clash struct {
	Time         float64
	Participants []ClashParticipant
}
