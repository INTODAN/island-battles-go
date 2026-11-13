package handlers_test

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"reflect"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/humatest"
	"github.com/google/uuid"

	"islandBattles/backend/handlers"
	"islandBattles/backend/internal/testutils"
	"islandBattles/backend/models"
	"islandBattles/backend/services"
	"islandBattles/lobby/domain"
)

// A browser asks for gzip, and Lambda streams a response's first 6 MB at
// full speed and the rest slowly, so the demo is packed for every client
// that accepts it, and plain for the others.
func TestTheDemoIsPackedWithGzipForClientsThatAcceptIt(t *testing.T) {
	database := testutils.InitTestDB(t)
	playerID, lobbyID, teamID := uuid.NewString(), uuid.NewString(), uuid.NewString()
	if _, err := services.RunLobbyGame(context.Background(), database, &testutils.RecordedPublisher{}, domain.Snapshot{
		Lobby: domain.Lobby{ID: lobbyID, GameKey: "island-battles", Phase: domain.PhaseStarted},
		Teams: []domain.Team{{ID: teamID, Order: 1, Name: "Blue"}}, StartRoster: domain.StartRoster{{PlayerID: playerID, TeamID: teamID}},
	}); err != nil {
		t.Fatal(err)
	}
	handler := handlers.LobbyGame{Database: database, Players: commandAuthentication{domain.Principal{PlayerID: playerID}}, Publisher: &testutils.RecordedPublisher{}}
	_, api := humatest.New(t)
	huma.Register(api, huma.Operation{OperationID: "demo", Method: http.MethodGet, Path: "/game/from-lobby/{lobby_id}/demo"}, handler.Demo)
	demoPath := "/game/from-lobby/" + lobbyID + "/demo"
	read := func(body []byte) models.Demo {
		t.Helper()
		var demo models.Demo
		if err := json.Unmarshal(body, &demo); err != nil {
			t.Fatalf("not a demo: %v", err)
		}
		if demo.Format != models.DemoFormat || len(demo.Frames) != 1 || len(demo.Logs) != 1 {
			t.Fatalf("demo = %+v", demo)
		}
		return demo
	}

	plain := api.Get(demoPath, "Authorization: Bearer valid")
	if plain.Code != http.StatusOK || plain.Header().Get("Content-Encoding") != "" || plain.Header().Get("Vary") != "Accept-Encoding" {
		t.Fatalf("plain demo=%d %v", plain.Code, plain.Header())
	}
	want := read(plain.Body.Bytes())
	for _, acceptEncoding := range []string{"gzip", "gzip, deflate, br, zstd", "br;q=1.0, GZIP;q=0.5", "deflate, gzip ; q=0.001"} {
		packed := api.Get(demoPath, "Authorization: Bearer valid", "Accept-Encoding: "+acceptEncoding)
		if packed.Code != http.StatusOK || packed.Header().Get("Content-Encoding") != "gzip" || packed.Header().Get("Vary") != "Accept-Encoding" {
			t.Fatalf("%q: demo=%d %v", acceptEncoding, packed.Code, packed.Header())
		}
		unpacker, err := gzip.NewReader(bytes.NewReader(packed.Body.Bytes()))
		if err != nil {
			t.Fatalf("%q: not gzip: %v", acceptEncoding, err)
		}
		unpacked, err := io.ReadAll(unpacker)
		if err != nil {
			t.Fatalf("%q: %v", acceptEncoding, err)
		}
		got := read(unpacked)
		got.SavedAt = want.SavedAt
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("%q: unpacked %+v, plain %+v", acceptEncoding, got, want)
		}
	}
	for _, acceptEncoding := range []string{"identity", "br, deflate", "gzip;q=0", "gzip; q=0.000", "x-gzip"} {
		response := api.Get(demoPath, "Authorization: Bearer valid", "Accept-Encoding: "+acceptEncoding)
		if response.Code != http.StatusOK || response.Header().Get("Content-Encoding") != "" {
			t.Fatalf("%q: demo=%d %v", acceptEncoding, response.Code, response.Header())
		}
		read(response.Body.Bytes())
	}
}
