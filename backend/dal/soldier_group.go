package dal

import (
	"islandBattles/backend/models"

	"github.com/jackc/pgx/v5/pgtype"
	"gorm.io/gorm"
)

// UpdateSoldierGroupPosition updates the given soldier group's position to the received position in the db.
func UpdateSoldierGroupPosition(db *gorm.DB, soldierGroup *models.SoldierGroup, newPosition pgtype.Point) error {
	soldierGroup.Position = newPosition
	return db.Save(soldierGroup).Error
}
