package dal

import (
	"time"

	"islandBattles/backend/models"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	packageIDColumn = clause.Column{Name: "package_id"}
	createdAtColumn = clause.Column{Name: "created_at"}
)

// CreateBotPackage stores a new package.
func CreateBotPackage(database *gorm.DB, record models.BotPackage) error {
	return database.Create(&record).Error
}

// FindBotPackage returns the package of a lobby, or nil when it has none
// with that ID.
func FindBotPackage(database *gorm.DB, lobbyID, packageID string) (*models.BotPackage, error) {
	var found []models.BotPackage
	if err := database.Where(&models.BotPackage{LobbyID: lobbyID, PackageID: packageID}).Limit(1).Find(&found).Error; err != nil {
		return nil, err
	}
	if len(found) == 0 {
		return nil, nil
	}
	return &found[0], nil
}

// CompleteBotPackage marks a pending package ready with what its check
// found, and deletes every other package of its owner in its lobby, so a
// member has one. It returns the deleted packages' keys, for their objects,
// and false when the package was no longer pending: deleted meanwhile, or
// completed by another request.
func CompleteBotPackage(database *gorm.DB, record models.BotPackage) (replacedKeys []string, completed bool, err error) {
	err = database.Transaction(func(transaction *gorm.DB) error {
		updated := transaction.Model(&models.BotPackage{}).
			Where(&models.BotPackage{PackageID: record.PackageID, LobbyID: record.LobbyID, Status: models.BotPackagePending}).
			Updates(map[string]any{
				"status": models.BotPackageReady, "owner_name": record.OwnerName, "language": record.Language,
				"entry_or_command": record.EntryOrCommand, "size_bytes": record.SizeBytes, "uploaded_at": record.UploadedAt,
			})
		if updated.Error != nil || updated.RowsAffected == 0 {
			return updated.Error
		}
		completed = true
		var replaced []models.BotPackage
		if err := transaction.Clauses(clause.Returning{Columns: []clause.Column{{Name: "s3_key"}}}).
			Where(&models.BotPackage{LobbyID: record.LobbyID, OwnerPlayerID: record.OwnerPlayerID}).
			Where(clause.Neq{Column: packageIDColumn, Value: record.PackageID}).
			Delete(&replaced).Error; err != nil {
			return err
		}
		for _, old := range replaced {
			replacedKeys = append(replacedKeys, old.S3Key)
		}
		return nil
	})
	if err != nil {
		return nil, false, err
	}
	return replacedKeys, completed, nil
}

// ListBotPackages returns a lobby's ready packages, newest first.
func ListBotPackages(database *gorm.DB, lobbyID string) ([]models.BotPackage, error) {
	var found []models.BotPackage
	err := database.Where(&models.BotPackage{LobbyID: lobbyID, Status: models.BotPackageReady}).
		Order(clause.OrderBy{Columns: []clause.OrderByColumn{{Column: clause.Column{Name: "uploaded_at"}, Desc: true}, {Column: packageIDColumn}}}).
		Find(&found).Error
	return found, err
}

// DeleteBotPackage deletes a lobby's package and reports whether it was
// there.
func DeleteBotPackage(database *gorm.DB, lobbyID, packageID string) (bool, error) {
	result := database.Where(&models.BotPackage{LobbyID: lobbyID, PackageID: packageID}).Delete(&models.BotPackage{})
	return result.RowsAffected == 1, result.Error
}

// DeleteExpiredBotPackages deletes up to limit packages created before
// before, pending or ready, oldest first, in one statement, and returns how
// many it deleted. Their zips are gone from the bucket by then: its
// lifecycle rule deletes them as long after their upload.
func DeleteExpiredBotPackages(database *gorm.DB, before time.Time, limit int) (int, error) {
	expired := database.Session(&gorm.Session{NewDB: true}).Model(&models.BotPackage{}).Select("package_id").
		Where(clause.Lt{Column: createdAtColumn, Value: before}).
		Order(clause.OrderByColumn{Column: createdAtColumn}).Limit(limit)
	result := database.Where(clause.Expr{SQL: "? IN (?)", Vars: []any{packageIDColumn, expired}}).Delete(&models.BotPackage{})
	return int(result.RowsAffected), result.Error
}
