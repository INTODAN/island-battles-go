package main

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/centrifugal/centrifuge-go"
)

func main() {
	endpoint := "ws://localhost:8000/connection/websocket"
	client := centrifuge.NewJsonClient(endpoint, centrifuge.Config{Token: "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiJhZG1pbiIsImV4cCI6MTc2MTMxMDM3MiwiaWF0IjoxNzYwNzA1NTcyfQ.QF_YZ8ta2Ou2E79q4YnJjHYjh2RZA4B4GZj5J4LJvkA"})
	defer client.Close()

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

	sub, err := client.NewSubscription("direct_ping")
	if err != nil {
		fmt.Println(err.Error())
		return
	}

	startTimeChan := make(chan time.Time, 1)
	programEnd := make(chan struct{})

	sub.OnPublication(func(event centrifuge.PublicationEvent) {
		go func() {
			start := <-startTimeChan

			var jsonData map[string]interface{}
			if err := json.Unmarshal(event.Data, &jsonData); err != nil {
				fmt.Println("Error unmarshalling json data:", err.Error())
				return
			}

			elapsed := time.Since(start)

			fmt.Println("Received new value:", jsonData["message"], "Time elapsed:", elapsed)
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

			startTimeChan <- time.Now()
			fmt.Println("\n\n\nRPC Proxy Pinging server")
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()

			emptyJsonRequestData, err := json.Marshal(&struct{}{})

			if ctx.Err() != nil {
				fmt.Println("Context is already canceled:", ctx.Err())
				return
			}

			_, err = client.RPC(ctx, "proxy_ping:", emptyJsonRequestData)

			if err != nil {
				fmt.Println("Error on RPC call:", err.Error())
				return
			}

			fmt.Println("Success!")
			programEnd <- struct{}{}
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

	<-programEnd
}
