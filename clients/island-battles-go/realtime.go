package islandbattles

import (
	"errors"

	"github.com/centrifugal/centrifuge-go"
)

// realtimeEvents are what a Client hears from its realtime connection. The
// connection calls them from its own goroutine, and they never block.
type realtimeEvents struct {
	// connected and disconnected report the connection, also on reconnects.
	connected    func()
	disconnected func()
	// subscribed reports the subscription's acknowledgment, also after a
	// resubscribe; subscribing and unsubscribed report that it was lost.
	subscribed   func()
	subscribing  func()
	unsubscribed func()
	// publication is a message published on the channel.
	publication func(data []byte)
}

// realtimeConnection is a Client's connection to Centrifugo, which pushes
// the player channel's events. It reconnects and resubscribes on its own.
type realtimeConnection interface {
	// Subscribe starts subscribing to a channel; the events report its
	// acknowledgment.
	Subscribe(channel string) error
	// Close disconnects for good.
	Close()
}

// realtimeDialer starts connecting to the websocket address with a token,
// and calls refreshToken when the server asks for a new one.
type realtimeDialer func(address, token string, refreshToken func() (string, error), events realtimeEvents) (realtimeConnection, error)

// centrifugoConnection is a realtimeConnection through centrifuge-go.
type centrifugoConnection struct {
	client *centrifuge.Client
	events realtimeEvents
}

// dialCentrifugo is the realtimeDialer of a Client.
func dialCentrifugo(address, token string, refreshToken func() (string, error), events realtimeEvents) (realtimeConnection, error) {
	client := centrifuge.NewJsonClient(address, centrifuge.Config{
		Token: token,
		GetToken: func(centrifuge.ConnectionTokenEvent) (string, error) {
			refreshed, err := refreshToken()
			if errors.Is(err, ErrAuthentication) {
				// Stops reconnecting: the credential is no longer valid.
				return "", centrifuge.ErrUnauthorized
			}
			return refreshed, err
		},
	})
	client.OnConnected(func(centrifuge.ConnectedEvent) { events.connected() })
	client.OnDisconnected(func(centrifuge.DisconnectedEvent) { events.disconnected() })
	if err := client.Connect(); err != nil {
		client.Close()
		return nil, err
	}
	return &centrifugoConnection{client: client, events: events}, nil
}

func (connection *centrifugoConnection) Subscribe(channel string) error {
	subscription, err := connection.client.NewSubscription(channel, centrifuge.SubscriptionConfig{Recoverable: true, Positioned: true})
	if err != nil {
		return err
	}
	events := connection.events
	subscription.OnSubscribed(func(centrifuge.SubscribedEvent) { events.subscribed() })
	subscription.OnSubscribing(func(centrifuge.SubscribingEvent) { events.subscribing() })
	subscription.OnUnsubscribed(func(centrifuge.UnsubscribedEvent) { events.unsubscribed() })
	subscription.OnPublication(func(event centrifuge.PublicationEvent) { events.publication(event.Data) })
	return subscription.Subscribe()
}

func (connection *centrifugoConnection) Close() { connection.client.Close() }
