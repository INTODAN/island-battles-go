package services_test

import (
	"context"
	"errors"
	"islandBattles/backend/internal/testutils"
	"islandBattles/backend/models"
	"islandBattles/backend/services"
	"testing"

	"github.com/centrifugal/gocent/v3"
	"github.com/google/uuid"
)

type recordedGenerationPublisher struct {
	err      error
	channels []string
	payloads [][]byte
}

func (publisher *recordedGenerationPublisher) Publish(ctx context.Context, channel string, payload []byte, _ ...gocent.PublishOption) (gocent.PublishResult, error) {
	if _, bounded := ctx.Deadline(); !bounded {
		return gocent.PublishResult{}, errors.New("publication has no deadline")
	}
	publisher.channels = append(publisher.channels, channel)
	publisher.payloads = append(publisher.payloads, append([]byte(nil), payload...))
	return gocent.PublishResult{}, publisher.err
}

func TestGenerationDeliveryRetriesCommittedNotifications(t *testing.T) {
	database := testutils.InitTestDB(t)
	playerID := uuid.NewString()
	notification := models.GenerationNotification{
		GameID: 1, GenerationID: 1, PlayerID: playerID,
		Payload: []byte(`{"schema_version":1,"type":"game.next_generation","state":{"generation_id":1}}`),
	}
	if err := database.Create(&notification).Error; err != nil {
		t.Fatal(err)
	}
	publisher := &recordedGenerationPublisher{err: errors.New("temporary publication failure")}
	worker := &services.GenerationDelivery{Database: database, Publisher: publisher}
	if err := worker.RunOnce(context.Background()); err == nil {
		t.Fatal("expected publication failure")
	}
	var saved models.GenerationNotification
	if err := database.First(&saved, notification.ID).Error; err != nil {
		t.Fatal(err)
	}
	if saved.PublishedAt != nil {
		t.Fatal("failed publication was acknowledged")
	}
	publisher.err = nil
	// A new worker proves the retry comes from persisted state.
	worker = &services.GenerationDelivery{Database: database, Publisher: publisher}
	if err := worker.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := database.First(&saved, notification.ID).Error; err != nil {
		t.Fatal(err)
	}
	if saved.PublishedAt == nil {
		t.Fatal("successful publication was not acknowledged")
	}
	if err := worker.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(publisher.channels) != 2 || publisher.channels[0] != "player:"+playerID || publisher.channels[1] != "player:"+playerID {
		t.Fatalf("unexpected publication channels: %v", publisher.channels)
	}
	if string(publisher.payloads[1]) != string(saved.Payload) {
		t.Fatalf("event payload changed: %s", publisher.payloads[1])
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := worker.RunOnce(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled delivery returned %v", err)
	}
}
