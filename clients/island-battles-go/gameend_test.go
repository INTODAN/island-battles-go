package islandbattles

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

// gameEndedJSON is a GameEndedDelivery of the fixture's board, whose demo
// link is demoURL.
func gameEndedJSON(t *testing.T, demoURL string) []byte {
	t.Helper()
	turn := FixtureTurn()
	data, err := json.Marshal(map[string]any{
		"$schema": "http://game.test/schemas/GameEndedDelivery.json",
		"game_id": 7, "team_color": 1, "turn_number": 91,
		"outcome":    map[string]any{"reason": "last_team_standing", "winner_team_color": 1, "summary": "Team 1 won", "departures": []any{}},
		"generation": turn.Generation, "memory": map[string]int{"turns": 90},
		"demo": map[string]any{"url": demoURL, "expires_at": "2026-11-28T12:00:00Z"},
	})
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestDecodeGameEndedDelivery(t *testing.T) {
	ended, err := DecodeGameEndedDelivery(gameEndedJSON(t, "https://game.test/game/7/demo?token=ibdt1.x"))
	if err != nil {
		t.Fatal(err)
	}
	if ended.GameID != 7 || ended.TeamColor != 1 || ended.TurnNumber != 91 || ended.Outcome.Reason != LastTeamStanding || *ended.Outcome.WinnerTeamColor != 1 ||
		ended.Generation.ID != 12 || string(ended.Memory) != `{"turns":90}` || ended.Demo.URL != "https://game.test/game/7/demo?token=ibdt1.x" ||
		!ended.Demo.ExpiresAt.Equal(time.Date(2026, 11, 28, 12, 0, 0, 0, time.UTC)) {
		t.Fatalf("got %+v", ended)
	}
	for _, broken := range []struct{ old, new, wanted string }{
		{`"demo":{`, `"demo_link":{`, "game_ended.demo is missing"},
		{`"memory":{"turns":90}`, `"memory":[1]`, "memory must be a JSON object"},
		{`"memory":{"turns":90}`, `"memory":null`, ""},
		{`"reason":"last_team_standing"`, `"reason":"surrender"`, "unknown outcome reason"},
		{`"game_id":7`, `"game_id":0`, "game_id must be at least 1"},
		{`"url":"https://game.test/game/7/demo?token=ibdt1.x"`, `"url":""`, "demo.url is empty"},
	} {
		data := strings.Replace(string(gameEndedJSON(t, "https://game.test/game/7/demo?token=ibdt1.x")), broken.old, broken.new, 1)
		_, err := DecodeGameEndedDelivery([]byte(data))
		if broken.wanted == "" && err != nil || broken.wanted != "" && (err == nil || !strings.Contains(err.Error(), broken.wanted)) {
			t.Errorf("%s: got %v, want %q", broken.new, err, broken.wanted)
		}
	}
}

// The backend's runners send this delivery to a worker; the SDK reads it.
func TestDecodeTheBackendsGameEndedDelivery(t *testing.T) {
	data, err := os.ReadFile("../../backend/botrunner/testdata/game_ended_event.json")
	if err != nil {
		t.Fatal(err)
	}
	var event struct {
		GameEnded json.RawMessage `json:"game_ended"`
	}
	if err := json.Unmarshal(data, &event); err != nil {
		t.Fatal(err)
	}
	ended, err := DecodeGameEndedDelivery(event.GameEnded)
	if err != nil || ended.GameID < 1 || ended.Demo.URL == "" {
		t.Fatalf("got %+v, %v", ended, err)
	}
}

// demoServer serves a demo at /game/7/demo?token=good, packed with gzip
// when the request accepts it, and records what it was asked.
func demoServer(t *testing.T, demo string) (*httptest.Server, *[]http.Header) {
	t.Helper()
	requests := &[]http.Header{}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		*requests = append(*requests, request.Header.Clone())
		switch {
		case request.URL.Path != "/game/7/demo":
			writeJSON(writer, http.StatusNotFound, map[string]any{"status": 404, "detail": "the game was deleted"})
		case request.URL.Query().Get("token") != "good":
			writeJSON(writer, http.StatusUnauthorized, map[string]any{"status": 401, "detail": "invalid demo token"})
		case strings.Contains(request.Header.Get("Accept-Encoding"), "gzip"):
			writer.Header().Set("Content-Type", "application/json")
			writer.Header().Set("Content-Encoding", "gzip")
			packer := gzip.NewWriter(writer)
			_, _ = packer.Write([]byte(demo))
			_ = packer.Close()
		default:
			writer.Header().Set("Content-Type", "application/json")
			_, _ = writer.Write([]byte(demo))
		}
	}))
	t.Cleanup(server.Close)
	return server, requests
}

func TestDownloadDemoUnpacksGzip(t *testing.T) {
	demo := `{"format":"island-battles-demo","frames":[` + strings.Repeat(`{"generation":1},`, 1000) + `{}]}`
	server, requests := demoServer(t, demo)
	got, err := DownloadDemo(context.Background(), DemoLink{URL: server.URL + "/game/7/demo?token=good"})
	if err != nil || string(got) != demo {
		t.Fatalf("got %d bytes, %v", len(got), err)
	}
	if header := (*requests)[0]; header.Get("Accept-Encoding") != "gzip" || header.Get("Authorization") != "" {
		t.Fatalf("asked with %v", header)
	}
	var file strings.Builder
	if err := WriteDemo(context.Background(), DemoLink{URL: server.URL + "/game/7/demo?token=good"}, &file); err != nil || file.String() != demo {
		t.Fatalf("wrote %d bytes, %v", file.Len(), err)
	}
}

func TestDownloadDemoReportsErrorsWithoutTheToken(t *testing.T) {
	server, _ := demoServer(t, "{}")
	_, err := DownloadDemo(context.Background(), DemoLink{URL: server.URL + "/game/7/demo?token=secret"})
	var gameError *GameError
	if !errors.Is(err, ErrAuthentication) || !errors.As(err, &gameError) || gameError.Status != 401 || !strings.Contains(err.Error(), "invalid demo token") || strings.Contains(err.Error(), "secret") {
		t.Fatalf("got %v", err)
	}
	if _, err := DownloadDemo(context.Background(), DemoLink{URL: server.URL + "/game/8/demo?token=good"}); !errors.Is(err, ErrRejected) || !strings.Contains(err.Error(), "deleted") {
		t.Fatalf("got %v", err)
	}
	if _, err := DownloadDemo(context.Background(), DemoLink{URL: "file:///etc/passwd"}); !errors.Is(err, ErrProtocol) {
		t.Fatalf("got %v", err)
	}
	closed := httptest.NewServer(http.NotFoundHandler())
	closed.Close()
	if _, err := DownloadDemo(context.Background(), DemoLink{URL: closed.URL + "/game/7/demo?token=secret"}); !errors.Is(err, ErrTransport) || strings.Contains(err.Error(), "secret") {
		t.Fatalf("got %v", err)
	}
}

func TestRunGameEndLogsAndReportsPanicsAndTimeouts(t *testing.T) {
	ended, err := DecodeGameEndedDelivery(gameEndedJSON(t, "https://game.test/game/7/demo?token=x"))
	if err != nil {
		t.Fatal(err)
	}
	var console strings.Builder
	outputs := &logOutputs{writers: []io.Writer{&console}}
	logs := func(ctx context.Context, ended GameEndedDelivery) error {
		Logger(ctx).Info("the game ended", "game", ended.GameID)
		return nil
	}
	if err := runGameEnd(context.Background(), logs, ended, outputs, time.Second); err != nil || !strings.Contains(console.String(), "INFO island_battles.player: the game ended game=7") {
		t.Fatalf("got %v, console %q", err, console.String())
	}
	panics := func(context.Context, GameEndedDelivery) error { panic("boom") }
	if err := runGameEnd(context.Background(), panics, ended, outputs, time.Second); err == nil || !strings.Contains(err.Error(), "panic: boom") {
		t.Fatalf("got %v", err)
	}
	release := make(chan struct{})
	defer close(release)
	stuck := func(context.Context, GameEndedDelivery) error {
		<-release
		return nil
	}
	if err := runGameEnd(context.Background(), stuck, ended, outputs, 20*time.Millisecond); err == nil || !strings.Contains(err.Error(), "took longer than 20ms") {
		t.Fatalf("got %v", err)
	}
}
