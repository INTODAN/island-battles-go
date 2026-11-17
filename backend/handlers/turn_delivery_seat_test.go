package handlers_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/INTODAN/mpl/domain"
	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/humatest"
	"github.com/google/uuid"
	"islandBattles/backend/handlers"
	"islandBattles/backend/internal/testutils"
	"islandBattles/backend/models"
	"islandBattles/backend/services"
	"islandBattles/backend/submittoken"
)

// The host at the start relays the localhost bot seats: they read each
// seat's turn with player_id, and the seat's bot submits it with the seat's
// token. Nobody else reads a seat's turn.
func TestTheHostReadsTheTurnsOfTheLocalhostBotSeatsTheyRelay(t *testing.T) {
	database := testutils.InitTestDB(t)
	host, bob, seat := uuid.NewString(), uuid.NewString(), uuid.NewString()
	lobbyID, red, blue, green := uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString()
	snapshot := domain.Snapshot{
		Lobby: domain.Lobby{ID: lobbyID, GameKey: "island-battles", Phase: domain.PhaseStarted, HostPlayerID: &host},
		Teams: []domain.Team{{ID: red, Order: 1}, {ID: blue, Order: 2}, {ID: green, Order: 3}},
		Members: []domain.Member{
			{PlayerID: host, DisplayName: "Alice", Active: true, MemberSettings: []byte(`{"bot":{"kind":"localhost"}}`)},
			{PlayerID: bob, DisplayName: "Bob", Active: true, MemberSettings: []byte(`{"bot":{"kind":"localhost","port":41415}}`)},
			{PlayerID: seat, DisplayName: "Localhost bot (port 41415)", Active: true, Kind: domain.MemberSeat, SeatSettings: []byte(`{"bot_localhost_port":"41415"}`)},
		},
		StartRoster: domain.StartRoster{{PlayerID: host, TeamID: red}, {PlayerID: bob, TeamID: blue}, {PlayerID: seat, TeamID: green}},
	}
	publisher := &testutils.RecordedPublisher{}
	if _, err := services.RunLobbyGame(context.Background(), database, publisher, nil, snapshot); err != nil {
		t.Fatal(err)
	}
	signer, err := submittoken.New([]byte("test-submit-token-secret"))
	if err != nil {
		t.Fatal(err)
	}
	handler := &handlers.LobbyGame{Database: database, Players: playerAuthentication{[]string{host, bob}}, Publisher: publisher, SubmitTokens: signer}
	_, api := humatest.New(t)
	huma.Register(api, huma.Operation{OperationID: "testDelivery", Method: "GET", Path: "/game/from-lobby/{lobby_id}/turn-delivery"}, handler.TurnDelivery)
	huma.Register(api, huma.Operation{OperationID: "registerGenerationCommand", Method: "POST", Path: "/generation/commands", SkipValidateBody: true}, handler.RegisterGenerationCommands)
	path := "/game/from-lobby/" + lobbyID + "/turn-delivery?player_id="

	response := api.Get(path+seat, "Authorization: Bearer "+host, "Host: example.com")
	if response.Code != 200 {
		t.Fatalf("seat delivery=%d %s", response.Code, response.Body.String())
	}
	var delivery models.TurnDelivery
	if err := json.Unmarshal(response.Body.Bytes(), &delivery); err != nil {
		t.Fatal(err)
	}
	claims, err := signer.Verify(delivery.Submit.Token, time.Now())
	if err != nil || claims.PlayerID != seat || delivery.Turn.TeamColor != 3 {
		t.Fatalf("seat delivery %+v, claims %+v, %v", delivery.Turn, claims, err)
	}
	// The host's own turn, without player_id or with their own.
	for _, own := range []string{"/game/from-lobby/" + lobbyID + "/turn-delivery", path + host} {
		response := api.Get(own, "Authorization: Bearer "+host)
		if response.Code != 200 || !strings.Contains(response.Body.String(), `"team_color":1`) {
			t.Fatalf("own delivery %s=%d %s", own, response.Code, response.Body.String())
		}
	}
	// Bob relays no seat, and the host relays the seat alone, not Bob.
	for name, attempt := range map[string][2]string{"Bob for the seat": {bob, seat}, "the host for Bob": {host, bob}, "the host for a stranger": {host, uuid.NewString()}} {
		if response := api.Get(path+attempt[1], "Authorization: Bearer "+attempt[0]); response.Code != 403 {
			t.Errorf("%s=%d %s", name, response.Code, response.Body.String())
		}
	}
	if response := api.Get(path+"not-a-uuid", "Authorization: Bearer "+host); response.Code != 422 {
		t.Errorf("a player_id that isn't a UUID=%d", response.Code)
	}
	// The seat's bot submits its turn with the seat's token.
	response = api.Post("/generation/commands", "Authorization: Bearer "+delivery.Submit.Token, strings.NewReader(batch(delivery.Turn.GameID, delivery.Turn.Generation.ID, "null")))
	var state models.GenerationState
	if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &state) != nil || state.PlayerID != seat || !state.Submitted || len(state.RelayedBots) != 0 {
		t.Fatalf("seat submit=%d %s", response.Code, response.Body.String())
	}
	if response := api.Get(path+seat, "Authorization: Bearer "+host); response.Code != 409 || !strings.Contains(response.Body.String(), "already complete") {
		t.Fatalf("completed seat delivery=%d %s", response.Code, response.Body.String())
	}
}
