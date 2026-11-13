package models

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"
)

// The runners' tests, in Go and in Python, share this example event, so it
// must stay exactly a BotRunnerEvent.
func TestBotRunnerEventExampleDecodesExactly(t *testing.T) {
	data, err := os.ReadFile("../botrunner/testdata/event.json")
	if err != nil {
		t.Fatal(err)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var event BotRunnerEvent
	if err := decoder.Decode(&event); err != nil {
		t.Fatalf("the example isn't a BotRunnerEvent: %v", err)
	}
	if event.Package == nil || event.Package.ID == "" || event.Package.URL == "" || event.Package.SizeBytes <= 0 {
		t.Fatalf("the example's package = %+v", event.Package)
	}
	if event.Hosted != nil {
		t.Fatal("the example is a package's event, without hosted")
	}
	if event.Limits == nil || *event.Limits != DefaultBotRunnerLimits {
		t.Fatalf("the example's limits = %+v, want the defaults %+v", event.Limits, DefaultBotRunnerLimits)
	}
	if event.Delivery.Turn.GameID == 0 || event.Delivery.Turn.Generation.ID == 0 || len(event.Delivery.Turn.Generation.Castles) == 0 {
		t.Fatalf("the example's turn = %+v", event.Delivery.Turn)
	}
	if len(event.Delivery.Turn.Memory) == 0 {
		t.Fatal("the example's turn needs memory, so the runners' tests see a pass keep it")
	}
	if event.Delivery.Submit.URL == "" || event.Delivery.Submit.Token == "" {
		t.Fatalf("the example's submit = %+v", event.Delivery.Submit)
	}
}

func TestBotRunnerEventOfAHostedBot(t *testing.T) {
	encoded, err := json.Marshal(BotRunnerEvent{Hosted: &BotRunnerHosted{URL: "https://bot.example.org/turn"}})
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &fields); err != nil {
		t.Fatal(err)
	}
	if _, found := fields["package"]; found {
		t.Errorf("a hosted bot's event has no package: %s", encoded)
	}
	if _, found := fields["limits"]; found {
		t.Errorf("a hosted bot's event has no limits: %s", encoded)
	}
	if string(fields["hosted"]) != `{"url":"https://bot.example.org/turn"}` {
		t.Errorf("hosted = %s", fields["hosted"])
	}
}
