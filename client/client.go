package client

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/centrifugal/centrifuge-go"
)

func Main() {
	endpoint := "ws://localhost:8000/connection/websocket"
	client := centrifuge.NewJsonClient(endpoint, centrifuge.Config{Token: "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiJhZG1pbiIsImV4cCI6MTc2MTMxMDM3MiwiaWF0IjoxNzYwNzA1NTcyfQ.QF_YZ8ta2Ou2E79q4YnJjHYjh2RZA4B4GZj5J4LJvkA"})

	client.OnConnecting(func(event centrifuge.ConnectingEvent) {
		go func() {
			fmt.Println("Connecting:", event.Code, event.Reason)
		}()
	})

	client.OnConnected(func(event centrifuge.ConnectedEvent) {
		go func() {
			fmt.Println("Connected: Client ID:", event.ClientID)
		}()
	})

	client.OnDisconnected(func(event centrifuge.DisconnectedEvent) {
		go func() {
			fmt.Println("Disconnected:", event.Code, event.Reason)
		}()
	})

	err := client.Connect()
	if err != nil {
		fmt.Println(err.Error())
		return
	}

	sub, err := client.NewSubscription("channel")
	if err != nil {
		fmt.Println(err.Error())
		return
	}

	sub.OnPublication(func(event centrifuge.PublicationEvent) {
		go func() {
			var jsonData map[string]interface{}
			if err := json.Unmarshal(event.Data, &jsonData); err != nil {
				fmt.Println("Error unmarshalling json data:", err.Error())
				return
			}

			fmt.Println("Received new value:", jsonData["value"])
		}()
	})

	sub.OnSubscribing(func(event centrifuge.SubscribingEvent) {
		go func() {
			fmt.Println("Subscribing:", event.Code, event.Reason)
		}()
	})

	sub.OnSubscribed(func(event centrifuge.SubscribedEvent) {
		go func() {
			fmt.Println("Subscribed to channel:", sub.Channel)
		}()
	})

	sub.OnUnsubscribed(func(event centrifuge.UnsubscribedEvent) {
		go func() {
			fmt.Println("Unsubscribed:", event.Code, event.Reason)
		}()
	})

	err = sub.Subscribe()
	if err != nil {
		fmt.Println(err.Error())
		return
	}

	time.Sleep(1000 * time.Second)
}
