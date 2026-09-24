package db_updates

import (
	"islandBattles/backend/dal"
	"islandBattles/backend/dal/commandHandlers/move_soldier_groups/internal"
	"islandBattles/backend/models"

	"github.com/jackc/pgx/v5/pgtype"
	"gorm.io/gorm"
)

func UpdateSoldierGroupPositions(database *gorm.DB, movements []internal.SoldierGroupMoveDelta, clashes []internal.Clash) error {
	positions := map[*models.SoldierGroup]pgtype.Point{}
	for _, clash := range clashes {
		for _, participant := range clash.Participants {
			positions[participant.SoldierGroup] = participant.Position
		}
	}
	for _, movement := range movements {
		position, stopped := positions[movement.SoldierGroup]
		if !stopped {
			position = movement.End
		}
		if err := dal.UpdateSoldierGroupPosition(database, movement.SoldierGroup, position); err != nil {
			return err
		}
	}
	return nil
}
