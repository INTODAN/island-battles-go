package services

import (
	"context"
	"errors"
	"islandBattles/backend/dal"
	"islandBattles/lobby/domain"
	"time"

	"github.com/centrifugal/gocent/v3"
	"gorm.io/gorm"
)

type GenerationPublisher interface {
	Publish(context.Context, string, []byte, ...gocent.PublishOption) (gocent.PublishResult, error)
}

// GenerationDelivery publishes committed game states on authenticated personal
// channels. A crash between publishing and acknowledgment can redeliver an event.
type GenerationDelivery struct {
	Database  *gorm.DB
	Publisher GenerationPublisher
}

func (worker *GenerationDelivery) RunOnce(ctx context.Context) error {
	notifications, err := dal.PendingGenerationNotifications(worker.Database.WithContext(ctx), 64)
	if err != nil {
		return err
	}
	var failures []error
	for _, notification := range notifications {
		if err := ctx.Err(); err != nil {
			return errors.Join(append(failures, err)...)
		}
		channel, err := domain.PlayerChannel(notification.PlayerID)
		if err == nil {
			publishContext, cancel := context.WithTimeout(ctx, 3*time.Second)
			_, err = worker.Publisher.Publish(publishContext, channel, notification.Payload)
			cancel()
		}
		if err == nil {
			err = dal.AcknowledgeGenerationNotification(worker.Database.WithContext(ctx), notification.ID, time.Now().UTC())
		}
		if err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}

func (worker *GenerationDelivery) Run(ctx context.Context, onError func(error)) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for ctx.Err() == nil {
		if err := worker.RunOnce(ctx); err != nil && onError != nil && ctx.Err() == nil {
			onError(err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
