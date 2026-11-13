package models

import "time"

// BotRunnerEvent is what the backend invokes a bot runner Lambda with, as
// JSON: one turn of one bot (#223). The runners are
// backend/botrunner/python (Python packages) and
// backend/cmd/bot-runner-native (native packages, and hosted bots). Both
// runners' tests read the example in backend/botrunner/testdata/event.json.
//
// An uploaded bot's event has Package and Limits: the runner loads the
// package, plays Delivery.Turn with it and submits the answer to
// Delivery.Submit itself, or a pass when the bot fails. A hosted bot's event
// has Hosted instead, which only the native runner takes: it POSTs Delivery
// to the bot's URL and does nothing more, since the bot submits for itself.
type BotRunnerEvent struct {
	Package  *BotRunnerPackage `json:"package,omitempty"`
	Hosted   *BotRunnerHosted  `json:"hosted,omitempty"`
	Delivery TurnDelivery      `json:"delivery"`
	Limits   *BotRunnerLimits  `json:"limits,omitempty"`
}

// BotRunnerPackage is the uploaded package a runner plays: the ready
// BotPackage, and a presigned GET of its zip, valid for an hour. The runner
// has no AWS permissions of its own.
type BotRunnerPackage struct {
	// ID is BotPackage.PackageID. A runner keeps the package it loaded last
	// and loads it again only for another ID.
	ID string `json:"id"`
	// URL is a presigned GET of the zip.
	URL string `json:"url"`
	// SizeBytes is BotPackage.SizeBytes, the zip's size.
	SizeBytes int64 `json:"size_bytes"`
}

// BotRunnerHosted is a hosted bot: the player's own server.
type BotRunnerHosted struct {
	// URL is where the runner POSTs the TurnDelivery.
	URL string `json:"url"`
}

// BotRunnerLimits are an uploaded bot's time limits, in seconds: at most
// LoadSeconds to download, unpack and start a package that isn't loaded
// yet, and at most PlaySeconds for the bot to answer the turn.
type BotRunnerLimits struct {
	LoadSeconds float64 `json:"load_seconds"`
	PlaySeconds float64 `json:"play_seconds"`
}

// DefaultBotRunnerLimits are the owner's budget for a turn (#112): 2 seconds
// of play, and up to 10 seconds to load a package.
var DefaultBotRunnerLimits = BotRunnerLimits{LoadSeconds: 10, PlaySeconds: 2}

// BotRunnerName names one of the two runners: python plays python packages,
// and native plays native packages and delivers hosted bots' turns.
type BotRunnerName string

const (
	BotRunnerPython BotRunnerName = "python"
	BotRunnerNative BotRunnerName = "native"
)

// BotRunnerPackageExpiry is how long a package's presigned GET in a
// BotRunnerEvent stays valid.
const BotRunnerPackageExpiry = time.Hour
