package client

import (
	"github.com/centrifugal/centrifuge-go"
)

func main() {
	endpoint := "ws://localhost:8000/connection/websocket"
	client := centrifuge.NewJsonClient(endpoint, centrifuge.Config{Token: ""})

	client.OnConnecting(func(event centrifuge.ConnectingEvent) {
		go func() {
			println("Connecting to ", event.Code, event.Reason)
		}()
	})

	client.OnConnected(func(event centrifuge.ConnectedEvent) {
		go func() {
			println("Connected! Client ID: ", event.ClientID)
		}()
	})

	client.OnDisconnected(func(event centrifuge.DisconnectedEvent) {
		go func() {
			println("Disconnected: ", event.Code, event.Reason)
		}()
	})

	err := client.Connect()
	if err != nil {
		println(err.Error())
		return
	}
}
