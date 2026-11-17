package handlers_test

import (
	"context"
	"encoding/json"
	"fmt"
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

// The player who added a localhost bot seat relays it: they read the seat's
// turn with player_id, and the seat's bot submits it with the seat's token.
// Nobody else reads a seat's turn, not even the host at the start, and a
// seat whose adder is not on the roster is relayed by nobody.
func TestTheAdderReadsTheTurnsOfTheLocalhostBotSeatsTheyRelay(t *testing.T) {
	database := testutils.InitTestDB(t)
	host, bob, gone := uuid.NewString(), uuid.NewString(), uuid.NewString()
	seat, bobsSeat, orphan := uuid.NewString(), uuid.NewString(), uuid.NewString()
	lobbyID := uuid.NewString()
	teams := []domain.Team{}
	for order := range 5 {
		teams = append(teams, domain.Team{ID: uuid.NewString(), Order: order + 1})
	}
	localhostSeat := func(port uint, adder string) []byte {
		return fmt.Appendf(nil, `{"bot_localhost_port":"%d","bot_relay_player_id":%q}`, port, adder)
	}
	snapshot := domain.Snapshot{
		Lobby: domain.Lobby{ID: lobbyID, GameKey: "island-battles", Phase: domain.PhaseStarted, HostPlayerID: &host},
		Teams: teams,
		Members: []domain.Member{
			{PlayerID: host, DisplayName: "Alice", Active: true, MemberSettings: []byte(`{"bot":{"kind":"localhost"}}`)},
			{PlayerID: bob, DisplayName: "Bob", Active: true, MemberSettings: []byte(`{"bot":{"kind":"localhost","port":41415}}`)},
			{PlayerID: seat, DisplayName: "Localhost bot (port 41415)", Active: true, Kind: domain.MemberSeat, SeatSettings: localhostSeat(41415, host)},
			// Bob added this seat while he was the host.
			{PlayerID: bobsSeat, DisplayName: "Localhost bot (port 41416)", Active: true, Kind: domain.MemberSeat, SeatSettings: localhostSeat(41416, bob)},
			// A former host who left added this one.
			{PlayerID: orphan, DisplayName: "Localhost bot (port 41417)", Active: true, Kind: domain.MemberSeat, SeatSettings: localhostSeat(41417, gone)},
		},
		StartRoster: domain.StartRoster{{PlayerID: host, TeamID: teams[0].ID}, {PlayerID: bob, TeamID: teams[1].ID}, {PlayerID: seat, TeamID: teams[2].ID},
			{PlayerID: bobsSeat, TeamID: teams[3].ID}, {PlayerID: orphan, TeamID: teams[4].ID}},
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
	huma.Register(api, huma.Operation{OperationID: "testGeneration", Method: "GET", Path: "/game/from-lobby/{lobby_id}/generation"}, handler.CurrentGeneration)
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
	// Bob relays the seat he added, and the host doesn't.
	if response := api.Get(path+bobsSeat, "Authorization: Bearer "+bob); response.Code != 200 || !strings.Contains(response.Body.String(), `"team_color":4`) {
		t.Fatalf("Bob's seat delivery=%d %s", response.Code, response.Body.String())
	}
	for name, attempt := range map[string][2]string{
		"Bob for the host's seat": {bob, seat}, "the host for Bob's seat": {host, bobsSeat}, "the host for Bob": {host, bob},
		"the host for the seat of a player who left": {host, orphan}, "Bob for the seat of a player who left": {bob, orphan}, "the host for a stranger": {host, uuid.NewString()},
	} {
		if response := api.Get(path+attempt[1], "Authorization: Bearer "+attempt[0]); response.Code != 403 {
			t.Errorf("%s=%d %s", name, response.Code, response.Body.String())
		}
	}
	if response := api.Get(path+"not-a-uuid", "Authorization: Bearer "+host); response.Code != 422 {
		t.Errorf("a player_id that isn't a UUID=%d", response.Code)
	}
	// Each relays the seats they added; the seat of a player who left is
	// nobody's.
	for _, relayer := range []struct{ playerID, seat string }{{host, seat}, {bob, bobsSeat}} {
		response := api.Get("/game/from-lobby/"+lobbyID+"/generation", "Authorization: Bearer "+relayer.playerID)
		var state models.GenerationState
		if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &state) != nil || len(state.RelayedBots) != 1 || state.RelayedBots[0].PlayerID != relayer.seat {
			t.Fatalf("relayed bots of %s=%d %s", relayer.playerID, response.Code, response.Body.String())
		}
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
