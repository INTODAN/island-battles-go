package testutils

import (
	"context"
	"encoding/json"
	"sync"
	"testing"

	"islandBattles/backend/models"
	"islandBattles/lobby/realtime"
)

// RecordedPublisher keeps what services push to Centrifugo after a commit.
// Err makes every push fail, as an unreachable Centrifugo would.
type RecordedPublisher struct {
	mutex        sync.Mutex
	publications []realtime.Publication
	Err          error
}

func (publisher *RecordedPublisher) Publish(_ context.Context, publications ...realtime.Publication) error {
	publisher.mutex.Lock()
	defer publisher.mutex.Unlock()
	publisher.publications = append(publisher.publications, publications...)
	return publisher.Err
}

func (publisher *RecordedPublisher) Publications() []realtime.Publication {
	publisher.mutex.Lock()
	defer publisher.mutex.Unlock()
	return append([]realtime.Publication{}, publisher.publications...)
}

// GenerationEvents decodes every pushed game.next_generation event in order.
func (publisher *RecordedPublisher) GenerationEvents(t *testing.T) []models.GenerationEvent {
	t.Helper()
	var events []models.GenerationEvent
	for _, event := range publisher.events(t, models.GenerationEventType) {
		events = append(events, models.GenerationEvent(event))
	}
	return events
}

// GameEndedEvents decodes every pushed game.ended event in order.
func (publisher *RecordedPublisher) GameEndedEvents(t *testing.T) []models.GameEndedEvent {
	t.Helper()
	return publisher.events(t, models.GameEndedEventType)
}

// events decodes the pushed events of one type, checking that each went to
// its player's channel. Both event types share one shape.
func (publisher *RecordedPublisher) events(t *testing.T, eventType string) []models.GameEndedEvent {
	t.Helper()
	var events []models.GameEndedEvent
	for _, publication := range publisher.Publications() {
		var event models.GameEndedEvent
		if err := json.Unmarshal(publication.Data, &event); err != nil {
			t.Fatal(err)
		}
		if publication.Channel != event.State.Channel {
			t.Fatalf("event for %s was pushed to %s", event.State.Channel, publication.Channel)
		}
		if event.Type == eventType {
			events = append(events, event)
		}
	}
	return events
}
