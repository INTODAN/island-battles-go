package lobbyadapter_test

import (
	"net/http"
	"testing"

	"github.com/INTODAN/mpl/domain"
	"github.com/google/uuid"
	"islandBattles/backend/dal"
	"islandBattles/backend/lobbyadapter"
	"islandBattles/backend/models"
)

func TestAMemberChoosesToPlayByHand(t *testing.T) {
	stack := newLobbyStack(t)
	alice, bob := stack.guest("Alice"), stack.guest("Bob")
	room := stack.room(lobbyadapter.GameKey, alice, bob)

	// The kind takes no other field.
	for _, extra := range []string{`"port":41414`, `"level":2`, `"url":"https://bot.example.com/turn"`, `"package_id":"` + uuid.NewString() + `"`} {
		var refusal struct {
			Code string `json:"code"`
		}
		stack.expect(stack.choose(room, alice, `{"kind":"manual",`+extra+`}`), http.StatusUnprocessableEntity, &refusal)
		if refusal.Code != "invalid_member_settings" {
			t.Errorf("manual with %s = %+v", extra, refusal)
		}
	}

	var chosen domain.Snapshot
	stack.expect(stack.choose(room, alice, `{"kind":"manual"}`), http.StatusOK, &chosen)
	if bots := stack.bots(chosen); !sameBot(bots[alice.playerID], botChoice(models.ManualBot, 0)) {
		t.Fatalf("Alice's choice in the snapshot = %+v", bots[alice.playerID])
	}
	stack.expect(stack.ready(room, alice), http.StatusOK, nil)
}

func TestAManualPlayerAgainstABotSeatStartsAndPlaysByHand(t *testing.T) {
	stack := newLobbyStack(t)
	alice := stack.guest("Alice")
	room := stack.soloRoom(lobbyadapter.GameKey, alice)
	stack.expect(stack.addSeat(room, alice, 1, map[string]any{"bot_level": 2}), http.StatusOK, nil)
	stack.expect(stack.choose(room, alice, `{"kind":"manual"}`), http.StatusOK, nil)
	stack.expect(stack.ready(room, alice), http.StatusOK, nil)
	stack.expect(stack.api.Post(stack.lobbyPath(room)+"/start", alice.header, map[string]any{}), http.StatusOK, nil)

	state := stack.state(room, alice)
	if !sameBot(&state.Bot, botChoice(models.ManualBot, 0)) || state.Bot.Port != nil || state.Submitted {
		t.Fatalf("the manual player's state = %+v", state)
	}
	players, err := dal.LoadFrozenPlayers(stack.database, state.GameID)
	if err != nil || len(players) != 2 {
		t.Fatalf("frozen players = %+v, %v", players, err)
	}
	for _, player := range players {
		if player.PlayerID == alice.playerID && (player.BotKind != models.ManualBot || player.BotPort != nil || player.BotLevel != nil || player.BotURL != nil || player.BotPackageID != nil || player.BotRelayPlayerID != nil) {
			t.Fatalf("Alice's frozen player = %+v", player)
		}
	}
	// The backend played the seat's turn and nothing for the manual player.
	inputs, err := dal.LoadGenerationInputs(stack.database, state.GenerationID)
	if err != nil {
		t.Fatal(err)
	}
	for _, input := range inputs {
		if input.PlayerID == alice.playerID {
			t.Fatalf("the manual player has an input before sending anything: %+v", input)
		}
	}

	// Their client sends the commands, as any roster player may.
	complete := true
	var advanced models.GenerationState
	stack.expect(stack.api.Post("/generation/commands", stack.playerToken(alice, lobbyadapter.GameKey), models.RegisterGenerationCommands{
		GameID: state.GameID, GenerationID: state.GenerationID, RequestID: uuid.NewString(), Complete: &complete, Commands: []models.PublicGenerationCommand{},
	}), http.StatusOK, &advanced)
	if advanced.GenerationID == state.GenerationID {
		t.Fatalf("the turn didn't advance after the manual player completed it: %+v", advanced)
	}
}
