package dal

import (
	"islandBattles/backend/models"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// PendingGenerationNotifications returns a bounded batch of committed events.
// Concurrent workers may publish the same event; consumers deduplicate generations.
func PendingGenerationNotifications(database *gorm.DB, limit int) ([]models.GenerationNotification, error) {
	var notifications []models.GenerationNotification
	err := database.Where(clause.Eq{Column: clause.Column{Name: "published_at"}, Value: nil}).
		Order(clause.OrderByColumn{Column: clause.Column{Name: "id"}}).
		Limit(limit).Find(&notifications).Error
	return notifications, err
}

func AcknowledgeGenerationNotification(database *gorm.DB, notificationID uint, publishedAt time.Time) error {
	return database.Model(&models.GenerationNotification{}).
		Where(&models.GenerationNotification{ID: notificationID}).
		Where(clause.Eq{Column: clause.Column{Name: "published_at"}, Value: nil}).
		Updates(&models.GenerationNotification{PublishedAt: &publishedAt}).Error
}
