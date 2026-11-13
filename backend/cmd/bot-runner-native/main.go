// Command bot-runner-native is the native bot runner (#223): a Lambda
// function that plays one turn of one uploaded native bot package, such as a
// Go bot, per invocation, and submits it with the turn's submit token. It
// also delivers a hosted bot's turn to the bot's own server. Its event is
// models.BotRunnerEvent. Build it for the provided.al2023 runtime on arm64:
//
//	GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -tags lambda.norpc -trimpath -ldflags='-s -w' -o bootstrap ./backend/cmd/bot-runner-native
//
// GAME_SUBMIT_HOST names the game's host, the only one it submits to.
//
// With --local it plays one turn of a local game instead, with a package on
// this machine (backend/README.md, "Bot runners"):
//
//	go run ./backend/cmd/bot-runner-native --local --package bot.zip --game-url http://localhost:8888 --lobby-id <id> --token=<player token>
package main

import (
	"os"

	"islandBattles/backend/observability"

	"github.com/aws/aws-lambda-go/lambda"
)

func main() {
	if len(os.Args) > 1 {
		os.Exit(runLocal(os.Args[1:], os.Stdout, os.Stderr))
	}
	// Warnings and errors only, as JSON lines that Lambda's level filter
	// reads, within the backend's per-instance log limits.
	logger, _ := observability.NewLambdaLogger(os.Stdout, observability.DefaultSettings())
	runner := &Runner{Directory: "/tmp/bot", SubmitHost: os.Getenv("GAME_SUBMIT_HOST"), Logger: logger}
	lambda.Start(runner.Handle)
}
