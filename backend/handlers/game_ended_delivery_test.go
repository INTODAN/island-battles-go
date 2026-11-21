package handlers_test

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"
	"islandBattles/backend/dal"
	"islandBattles/backend/demotoken"
	"islandBattles/backend/models"
	"islandBattles/backend/services"
	"islandBattles/backend/submittoken"
)

// A localhost bot's client reads the end of its game once it has an outcome,
// and the demo link in it downloads the game's demo with no other credential,
// for its own game only (#413).
func TestTheEndOfAGameAndItsDemoLink(t *testing.T) {
	api, submitTokens, players, lobbyID, handler := deliveryGame(t)
	demos, err := demotoken.New([]byte("test-submit-token-secret"))
	if err != nil {
		t.Fatal(err)
	}
	handler.Demos = services.DemoLinks{Tokens: demos}
	huma.Register(api, huma.Operation{OperationID: "testGameEnded", Method: "GET", Path: "/game/from-lobby/{lobby_id}/game-ended-delivery"}, handler.GameEndedDelivery)
	huma.Register(api, huma.Operation{OperationID: "testGameDemo", Method: "GET", Path: "/game/{id}/demo"}, handler.GameDemo)
	path := "/game/from-lobby/" + lobbyID + "/game-ended-delivery"
	if response := api.Get(path); response.Code != 401 {
		t.Fatalf("unauthenticated=%d", response.Code)
	}
	if response := api.Get(path, "Authorization: Bearer "+players[1]); response.Code != 409 || !strings.Contains(response.Body.String(), services.GameNotEndedProblemType) {
		t.Fatalf("a running game=%d %s", response.Code, response.Body.String())
	}
	turn := readDelivery(t, api, lobbyID, players[0]).Turn
	game, err := dal.FindGame(handler.Database, turn.GameID)
	if err != nil {
		t.Fatal(err)
	}
	winner := uint(1)
	if err := dal.SaveGameOutcome(handler.Database, game, models.GameOutcome{Reason: models.GameOutcomeLastTeamStanding, WinnerTeamColor: &winner}); err != nil {
		t.Fatal(err)
	}
	handler.PublicURL = "https://island-battles.example.org"
	response := api.Get(path, "Authorization: Bearer "+players[1])
	if response.Code != 200 {
		t.Fatalf("ended=%d %s", response.Code, response.Body.String())
	}
	var ended models.GameEndedDelivery
	if err := json.Unmarshal(response.Body.Bytes(), &ended); err != nil {
		t.Fatal(err)
	}
	if ended.GameID != turn.GameID || ended.TeamColor != 2 || ended.TurnNumber != 1 || ended.Outcome.Reason != models.GameOutcomeLastTeamStanding || ended.Generation.ID != turn.Generation.ID {
		t.Fatalf("ended=%+v", ended)
	}
	prefix := fmt.Sprintf("https://island-battles.example.org/game/%d/demo?token=", turn.GameID)
	if !strings.HasPrefix(ended.Demo.URL, prefix) || !ended.Demo.ExpiresAt.After(time.Now().Add(6*24*time.Hour)) {
		t.Fatalf("demo=%+v", ended.Demo)
	}
	link, _ := url.Parse(ended.Demo.URL)
	token := link.Query().Get("token")
	demoPath := fmt.Sprintf("/game/%d/demo", turn.GameID)

	for name, attempt := range map[string]struct {
		path    string
		headers []any
	}{
		"the link":         {demoPath + "?" + link.RawQuery, nil},
		"a bearer token":   {demoPath, []any{"Authorization: Bearer " + token}},
		"a player's token": {demoPath, []any{"Authorization: Bearer " + players[0]}},
	} {
		response := api.Get(attempt.path, attempt.headers...)
		if response.Code != 200 || !strings.HasPrefix(response.Header().Get("Content-Disposition"), "attachment") {
			t.Fatalf("%s=%d %s", name, response.Code, response.Body.String())
		}
		var demo models.Demo
		if err := json.Unmarshal(response.Body.Bytes(), &demo); err != nil || demo.GameID != turn.GameID || demo.Outcome == nil {
			t.Fatalf("%s: demo %+v, %v", name, demo.Outcome, err)
		}
	}

	submit, err := submitTokens.Issue(submittoken.Claims{GameID: turn.GameID, PlayerID: players[1], GenerationID: turn.Generation.ID, Expires: time.Now().Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	other, _ := demotoken.New([]byte("another secret"))
	forged, _ := other.Issue(demotoken.Claims{GameID: turn.GameID, PlayerID: players[1], Expires: time.Now().Add(time.Hour)})
	expired, _ := demos.Issue(demotoken.Claims{GameID: turn.GameID, PlayerID: players[1], Expires: time.Now().Add(-time.Second)})
	stranger, _ := demos.Issue(demotoken.Claims{GameID: turn.GameID, PlayerID: uuid.NewString(), Expires: time.Now().Add(time.Hour)})
	for name, attempt := range map[string]struct {
		path    string
		headers []any
		status  int
	}{
		"another game":       {fmt.Sprintf("/game/%d/demo?token=%s", turn.GameID+1, url.QueryEscape(token)), nil, 401},
		"another secret":     {demoPath + "?token=" + url.QueryEscape(forged), nil, 401},
		"an expired token":   {demoPath + "?token=" + url.QueryEscape(expired), nil, 401},
		"a stranger's token": {demoPath + "?token=" + url.QueryEscape(stranger), nil, 401},
		"a submit token":     {demoPath + "?token=" + url.QueryEscape(submit), nil, 401},
		"a submit bearer":    {demoPath, []any{"Authorization: Bearer " + submit}, 401},
		"no credential":      {demoPath, nil, 401},
	} {
		if response := api.Get(attempt.path, attempt.headers...); response.Code != attempt.status {
			t.Errorf("%s=%d %s", name, response.Code, response.Body.String())
		}
	}
	// The demo token works for no other endpoint.
	if response := api.Get("/game/from-lobby/"+lobbyID+"/turn-delivery", "Authorization: Bearer "+token); response.Code != 401 {
		t.Fatalf("the demo token read a turn delivery=%d", response.Code)
	}
	if response := api.Post("/generation/commands", "Authorization: Bearer "+token, strings.NewReader(batch(turn.GameID, turn.Generation.ID, "null"))); response.Code != 401 {
		t.Fatalf("the demo token submitted=%d", response.Code)
	}
}
