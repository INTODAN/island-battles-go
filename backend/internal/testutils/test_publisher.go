package testutils

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"

	"islandBattles/backend/models"
	"islandBattles/backend/services"
)

// RecordedPublisher keeps what services push to Centrifugo after a commit.
// Err makes every push fail, as an unreachable Centrifugo would.
type RecordedPublisher struct {
	mutex        sync.Mutex
	publications []services.Publication
	Err          error
}

func (publisher *RecordedPublisher) Publish(_ context.Context, publications ...services.Publication) error {
	publisher.mutex.Lock()
	defer publisher.mutex.Unlock()
	publisher.publications = append(publisher.publications, publications...)
	return publisher.Err
}

// Publications returns what was pushed to players' channels, in order.
// WatchEvents returns what went to watch channels.
func (publisher *RecordedPublisher) Publications() []services.Publication {
	return publisher.filter(func(channel string) bool { return !strings.HasPrefix(channel, "watch:") })
}

// Pushes returns every push, watch channels included, in order.
func (publisher *RecordedPublisher) Pushes() []services.Publication {
	return publisher.filter(func(string) bool { return true })
}

func (publisher *RecordedPublisher) filter(keep func(channel string) bool) []services.Publication {
	publisher.mutex.Lock()
	defer publisher.mutex.Unlock()
	kept := []services.Publication{}
	for _, publication := range publisher.publications {
		if keep(publication.Channel) {
			kept = append(kept, publication)
		}
	}
	return kept
}

// WatchEvents decodes every event pushed to a watch channel, of either type,
// in order, checking that each went to its state's channel.
func (publisher *RecordedPublisher) WatchEvents(t *testing.T) []models.WatchGameEndedEvent {
	t.Helper()
	var events []models.WatchGameEndedEvent
	for _, publication := range publisher.filter(func(channel string) bool { return strings.HasPrefix(channel, "watch:") }) {
		var event models.WatchGameEndedEvent
		if err := json.Unmarshal(publication.Data, &event); err != nil {
			t.Fatal(err)
		}
		if publication.Channel != event.State.Channel {
			t.Fatalf("watch event for %s was pushed to %s", event.State.Channel, publication.Channel)
		}
		events = append(events, event)
	}
	return events
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
