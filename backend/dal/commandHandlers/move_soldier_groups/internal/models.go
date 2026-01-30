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
