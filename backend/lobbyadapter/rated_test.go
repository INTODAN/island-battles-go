package lobbyadapter_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/INTODAN/mpl/domain"
	lobbypostgres "github.com/INTODAN/mpl/postgres"
	"islandBattles/backend/dal"
	"islandBattles/backend/internal/testutils"
	"islandBattles/backend/lobbyadapter"
	"islandBattles/backend/models"
	"islandBattles/backend/services"

	"github.com/google/uuid"
)

// ratedSettings are the game settings of a lobby of teams on the circle map,
// rated or not.
func ratedSettings(teams int, rated bool) map[string]any {
	return map[string]any{"map": testutils.CircleMap(teams), "rated": rated}
}

// player signs a Google account in to the lobby, as a new session of it
// when the account exists, and returns the session as a player with a client
// instance.
func (stack *lobbyStack) player(subject, displayName string) guest {
	stack.t.Helper()
	signedIn, _ := stack.signIn(subject, displayName)
	signedIn.client = uuid.NewString()
	return signedIn
}

// lobbyOf creates an Island Battles lobby of teams with settings, with host
// on the first team.
func (stack *lobbyStack) lobbyOf(host guest, teams int, settings map[string]any) domain.Snapshot {
	stack.t.Helper()
	names := make([]map[string]any, teams)
	for index := range names {
		names[index] = map[string]any{"name": fmt.Sprintf("Team %d", index+1)}
	}
	var snapshot domain.Snapshot
	stack.expect(stack.api.Post("/lobby/v1/lobbies", host.header, map[string]any{
		"request_id": uuid.NewString(), "game_key": lobbyadapter.GameKey, "name": "Rated", "teams": names,
		"client_instance_id": host.client, "game_settings": settings,
	}), http.StatusCreated, &snapshot)
	stack.expect(stack.api.Put(stack.lobbyPath(snapshot)+"/team", host.header, map[string]any{"team_id": snapshot.Teams[0].ID}), http.StatusOK, nil)
	return snapshot
}

// registerBotName registers a bot name for a signed-in player.
func (stack *lobbyStack) registerBotName(member guest, name string) models.CompetitorView {
	stack.t.Helper()
	var competitor models.CompetitorView
	stack.expect(stack.api.Post("/account/bot-names", stack.playerToken(member, lobbyadapter.GameKey), map[string]string{"name": name}), http.StatusCreated, &competitor)
	return competitor
}

// expectRequirement checks that response is the lobby's game_requirement of
// problemType and returns its detail.
func (stack *lobbyStack) expectRequirement(response *httptest.ResponseRecorder, problemType string) string {
	stack.t.Helper()
	var refusal gameRequirement
	stack.expect(response, http.StatusConflict, &refusal)
	if refusal.Code != "game_requirement" || refusal.Type != problemType {
		stack.t.Fatalf("refusal = %+v, want game_requirement of type %s", refusal, problemType)
	}
	return refusal.Detail
}

// expectInvalid checks that response is a 422 with code and returns its
// detail.
func (stack *lobbyStack) expectInvalid(response *httptest.ResponseRecorder, code string) string {
	stack.t.Helper()
	var refusal problem
	stack.expect(response, http.StatusUnprocessableEntity, &refusal)
	if refusal.Code != code {
		stack.t.Fatalf("refusal = %+v, want %s", refusal, code)
	}
	return refusal.Detail
}

func (stack *lobbyStack) start(room domain.Snapshot, host guest) *httptest.ResponseRecorder {
	return stack.api.Post(stack.lobbyPath(room)+"/start", host.header, map[string]any{})
}

// setRated changes a lobby's rated setting in the lobby's own database, as
// if the host had changed it after its members got ready, which the API
// can't reproduce, since a change of settings clears every member's ready.
func (stack *lobbyStack) setRated(room domain.Snapshot, rated bool) {
	stack.t.Helper()
	settings := testutils.CircleMapSettings(len(room.Teams), fmt.Sprintf(`{"rated":%t}`, rated))
	if err := stack.lobbyDatabase.Model(&lobbypostgres.RoomRecord{}).Where(&lobbypostgres.RoomRecord{ID: room.Lobby.ID}).Update("game_settings", []byte(settings)).Error; err != nil {
		stack.t.Fatal(err)
	}
}

func TestALobbyIsRatedOnlyWhenItsSettingsSaySo(t *testing.T) {
	stack := newLobbyStack(t)
	dana := stack.player("google-dana", "Dana")
	rated := stack.lobbyOf(dana, 2, ratedSettings(2, true))
	if !strings.Contains(string(rated.Lobby.GameSettings), `"rated":true`) || !services.LobbyRated(rated.Lobby.GameSettings) {
		t.Fatalf("game_settings = %s", rated.Lobby.GameSettings)
	}
	unrated := stack.lobbyOf(dana, 2, map[string]any{"map": testutils.CircleMap(2)})
	if services.LobbyRated(unrated.Lobby.GameSettings) {
		t.Fatalf("game_settings = %s", unrated.Lobby.GameSettings)
	}
	for _, invalid := range []map[string]any{
		{"map": testutils.CircleMap(2), "rated": true, "ranked": true},
		{"map": testutils.CircleMap(2), "rated": "yes"},
		{"map": testutils.CircleMap(2), "rated": nil},
	} {
		detail := stack.expectInvalid(stack.api.Post("/lobby/v1/lobbies", dana.header, map[string]any{
			"request_id": uuid.NewString(), "game_key": lobbyadapter.GameKey, "name": "Rated", "teams": teamBodies("Blue", "Red"),
			"client_instance_id": dana.client, "game_settings": invalid,
		}), "invalid_game_settings")
		if !strings.HasPrefix(detail, "game_settings.ra") {
			t.Fatalf("settings %v = %q", invalid, detail)
		}
	}
}

// In a rated lobby a member gets ready only signed in, and playing by hand
// or with a bot under one of their bot names.
func TestARatedLobbysReadyGate(t *testing.T) {
	stack := newLobbyStack(t)
	dana, guestPlayer := stack.player("google-dana", "Dana"), stack.guest("Gus")
	kraken := stack.registerBotName(dana, "Kraken")
	room := stack.lobbyOf(dana, 2, ratedSettings(2, true))
	stack.join(room, guestPlayer, 1)

	// A guest can't play, whatever they choose.
	for _, bot := range []string{`{"kind":"localhost"}`, `{"kind":"manual"}`} {
		stack.expect(stack.choose(room, guestPlayer, bot), http.StatusOK, nil)
		if detail := stack.expectRequirement(stack.ready(room, guestPlayer), services.RatedSignInRequiredProblemType); detail != "This lobby is rated. Sign in with Google in the 3D client to play in it." {
			t.Fatalf("a guest's refusal = %q", detail)
		}
	}

	// A signed-in member needs a bot name for a bot, and can't choose a
	// default bot.
	for _, bot := range []string{`{"kind":"default","level":2}`, `{"kind":"localhost"}`, `{"kind":"localhost","port":41415}`} {
		stack.expect(stack.choose(room, dana, bot), http.StatusOK, nil)
		if detail := stack.expectRequirement(stack.ready(room, dana), services.RatedBotNameRequiredProblemType); detail != "This lobby is rated. Play by hand, or choose one of your bot names for your bot." {
			t.Fatalf("%s = %q", bot, detail)
		}
	}
	// A bot name of theirs is compared without regard to case and stored as
	// given; playing by hand needs none.
	var chosen domain.Snapshot
	stack.expect(stack.choose(room, dana, `{"kind":"localhost","name":"kraken"}`), http.StatusOK, &chosen)
	if settings := string(memberOf(t, chosen, dana.playerID).MemberSettings); settings != `{"bot":{"kind":"localhost","name":"kraken"}}` {
		t.Fatalf("Dana's settings = %s", settings)
	}
	stack.expect(stack.ready(room, dana), http.StatusOK, nil)
	stack.expect(stack.choose(room, dana, `{"kind":"manual"}`), http.StatusOK, nil)
	stack.expect(stack.ready(room, dana), http.StatusOK, nil)

	// A name deleted after it was chosen no longer gets them ready.
	stack.expect(stack.choose(room, dana, `{"kind":"localhost","name":"Kraken"}`), http.StatusOK, nil)
	stack.expect(stack.api.Delete("/account/bot-names/"+kraken.ID, stack.playerToken(dana, lobbyadapter.GameKey)), http.StatusNoContent, nil)
	if detail := stack.expectRequirement(stack.ready(room, dana), services.RatedBotNameGoneProblemType); detail != "Your bot name Kraken no longer exists. Choose another." {
		t.Fatalf("a deleted name's refusal = %q", detail)
	}
}

// A bot's name must be one of the member's own, in every lobby; a guest has
// none, and only a bot that a client or the server runs takes one.
func TestABotNameMustBeOneOfYours(t *testing.T) {
	stack := newLobbyStack(t)
	dana, erin, guestPlayer := stack.player("google-dana", "Dana"), stack.player("google-erin", "Erin"), stack.guest("Gus")
	stack.registerBotName(dana, "Kraken")
	for _, rated := range []bool{true, false} {
		room := stack.lobbyOf(dana, 3, ratedSettings(3, rated))
		stack.join(room, erin, 1)
		stack.join(room, guestPlayer, 2)
		for _, member := range []guest{erin, guestPlayer} {
			if detail := stack.expectInvalid(stack.choose(room, member, `{"kind":"localhost","name":"Kraken"}`), "invalid_member_settings"); detail != "Kraken isn't one of your bot names." {
				t.Fatalf("rated %t: another's name = %q", rated, detail)
			}
		}
		for _, bot := range []string{`{"kind":"manual","name":"Kraken"}`, `{"kind":"default","level":2,"name":"Kraken"}`} {
			stack.expectInvalid(stack.choose(room, dana, bot), "invalid_member_settings")
		}
		stack.expect(stack.choose(room, dana, `{"kind":"localhost","name":"KRAKEN"}`), http.StatusOK, nil)
		stack.expect(stack.ready(room, dana), http.StatusOK, nil)
	}
}

// In a rated lobby every bot seat is a default bot level, each level once.
func TestARatedLobbyTakesOnlyDefaultBotSeatsEachLevelOnce(t *testing.T) {
	stack := newServerBotStack(t)
	dana := stack.player("google-dana", "Dana")
	room := stack.lobbyOf(dana, 3, ratedSettings(3, true))
	for _, seat := range []map[string]any{{"bot_url": "https://bot.example.com/turn"}, localhostSeat(41415, dana)} {
		if detail := stack.expectInvalid(stack.addSeat(room, dana, 1, seat), "invalid_seat_settings"); detail != services.RatedSeatRule {
			t.Fatalf("seat %v = %q", seat, detail)
		}
	}
	stack.expect(stack.addSeat(room, dana, 1, map[string]any{"bot_level": 2}), http.StatusOK, nil)
	if detail := stack.expectInvalid(stack.addSeat(room, dana, 2, map[string]any{"bot_level": 2}), "invalid_seat_settings"); detail != services.RatedSeatRule+" This lobby already has Bot (level 2)." {
		t.Fatalf("a second level 2 seat = %q", detail)
	}
	stack.expect(stack.addSeat(room, dana, 2, map[string]any{"bot_level": 3}), http.StatusOK, nil)

	// Rated can't be turned on while a seat breaks the rule; off, it can.
	unrated := stack.lobbyOf(dana, 2, ratedSettings(2, false))
	stack.expect(stack.addSeat(unrated, dana, 1, map[string]any{"bot_url": "https://bot.example.com/turn"}), http.StatusOK, nil)
	path := stack.lobbyPath(unrated) + "/settings"
	detail := stack.expectInvalid(stack.api.Put(path, dana.header, map[string]any{"teams": teamBodies("Blue", "Red"), "game_settings": ratedSettings(2, true)}), "invalid_game_settings")
	if detail != "game_settings.rated: "+services.RatedSeatRule+" Remove Hosted bot." {
		t.Fatalf("turning rated on = %q", detail)
	}
	stack.expect(stack.api.Put(path, dana.header, map[string]any{"teams": teamBodies("Blue", "Red"), "game_settings": ratedSettings(2, false)}), http.StatusOK, nil)
	seat := seatOn(t, stack.snapshot(unrated, dana), 1)
	stack.expect(stack.api.Delete(stack.lobbyPath(unrated)+"/seats/"+seat.ID, dana.header), http.StatusOK, nil)
	stack.expect(stack.addSeat(unrated, dana, 1, map[string]any{"bot_level": 1}), http.StatusOK, nil)
	stack.expect(stack.api.Put(path, dana.header, map[string]any{"teams": teamBodies("Blue", "Red"), "game_settings": ratedSettings(2, true)}), http.StatusOK, nil)
}

// Two sessions of one account are two members, who each get ready, but a
// rated game needs a different account on each team.
func TestARatedStartNeedsADifferentAccountOnEachTeam(t *testing.T) {
	stack := newLobbyStack(t)
	laptop, phone := stack.player("google-dana", "Dana"), stack.player("google-dana", "Dana")
	if laptop.playerID == phone.playerID {
		t.Fatal("the two sessions are one player")
	}
	room := stack.lobbyOf(laptop, 2, ratedSettings(2, true))
	stack.join(room, phone, 1)
	for _, member := range []guest{laptop, phone} {
		stack.expect(stack.choose(room, member, `{"kind":"manual"}`), http.StatusOK, nil)
		stack.expect(stack.ready(room, member), http.StatusOK, nil)
	}
	if detail := stack.expectRequirement(stack.start(room, laptop), services.RatedSameAccountProblemType); detail != "Dana and Dana are the same account. A rated game needs a different account on each team." {
		t.Fatalf("the start's refusal = %q", detail)
	}
}

// The start checks the whole roster again, and names whom the host waits
// for: members who got ready before the lobby was rated, a name deleted
// since, and a seat added before.
func TestARatedStartChecksTheRosterAgain(t *testing.T) {
	stack := newLobbyStack(t)
	dana, erin, guestPlayer := stack.player("google-dana", "Dana"), stack.player("google-erin", "Erin"), stack.guest("Gus")
	kraken := stack.registerBotName(erin, "Kraken")
	readyRoom := func(rated bool, bots map[guest]string) domain.Snapshot {
		room := stack.lobbyOf(dana, len(bots), ratedSettings(len(bots), rated))
		team := 1
		for member, bot := range bots {
			if member != dana {
				stack.join(room, member, team)
				team++
			}
			stack.expect(stack.choose(room, member, bot), http.StatusOK, nil)
			stack.expect(stack.ready(room, member), http.StatusOK, nil)
		}
		return room
	}

	room := readyRoom(false, map[guest]string{dana: `{"kind":"manual"}`, guestPlayer: `{"kind":"localhost"}`})
	stack.setRated(room, true)
	if detail := stack.expectRequirement(stack.start(room, dana), services.RatedSignInRequiredProblemType); detail != "This lobby is rated, and Gus didn't sign in: only players signed in with Google in the 3D client can play in it." {
		t.Fatalf("a guest at the start = %q", detail)
	}

	room = readyRoom(false, map[guest]string{dana: `{"kind":"manual"}`, erin: `{"kind":"default","level":2}`})
	stack.setRated(room, true)
	if detail := stack.expectRequirement(stack.start(room, dana), services.RatedBotNameRequiredProblemType); detail != "This lobby is rated: waiting for Erin to play by hand, or to choose one of their bot names for their bot." {
		t.Fatalf("a default bot at the start = %q", detail)
	}

	room = readyRoom(true, map[guest]string{dana: `{"kind":"manual"}`, erin: `{"kind":"localhost","name":"Kraken"}`})
	stack.expect(stack.api.Delete("/account/bot-names/"+kraken.ID, stack.playerToken(erin, lobbyadapter.GameKey)), http.StatusNoContent, nil)
	if detail := stack.expectRequirement(stack.start(room, dana), services.RatedBotNameGoneProblemType); detail != "Erin's bot name Kraken no longer exists: they must choose another." {
		t.Fatalf("a deleted name at the start = %q", detail)
	}

	room = stack.lobbyOf(dana, 2, ratedSettings(2, false))
	stack.expect(stack.addSeat(room, dana, 1, localhostSeat(41415, dana)), http.StatusOK, nil)
	stack.expect(stack.choose(room, dana, `{"kind":"manual"}`), http.StatusOK, nil)
	stack.expect(stack.ready(room, dana), http.StatusOK, nil)
	stack.setRated(room, true)
	if detail := stack.expectRequirement(stack.start(room, dana), services.RatedSeatProblemType); detail != services.RatedSeatRule+" Remove Localhost bot (port 41415)." {
		t.Fatalf("a localhost seat at the start = %q", detail)
	}
	// Unrated again, the same lobby starts.
	stack.setRated(room, false)
	stack.expect(stack.start(room, dana), http.StatusOK, nil)
}

// A rated game records each team's competitor: a person's hand play or bot
// name, and a seat's house bot. The same lobby unrated records none.
func TestARatedGameRecordsItsCompetitors(t *testing.T) {
	stack := newLobbyStack(t)
	dana := stack.player("google-dana", "Dana")
	kraken := stack.registerBotName(dana, "Kraken")
	var account models.IslandAccountView
	stack.expect(stack.api.Get("/account", stack.playerToken(dana, lobbyadapter.GameKey)), http.StatusOK, &account)
	house, err := dal.FindOrCreateHouseCompetitor(stack.database, 2)
	if err != nil {
		t.Fatal(err)
	}
	for _, play := range []struct {
		bot        string
		rated      bool
		competitor string
		name       string
	}{
		{bot: `{"kind":"manual"}`, rated: true, competitor: account.Hand.ID},
		{bot: `{"kind":"localhost","name":"kraken"}`, rated: true, competitor: kraken.ID, name: "Kraken"},
		{bot: `{"kind":"manual"}`, rated: false},
		{bot: `{"kind":"localhost","name":"Kraken"}`, rated: false},
	} {
		room := stack.lobbyOf(dana, 2, ratedSettings(2, play.rated))
		stack.expect(stack.addSeat(room, dana, 1, map[string]any{"bot_level": 2}), http.StatusOK, nil)
		stack.expect(stack.choose(room, dana, play.bot), http.StatusOK, nil)
		stack.expect(stack.ready(room, dana), http.StatusOK, nil)
		stack.expect(stack.start(room, dana), http.StatusOK, nil)

		state := stack.state(room, dana)
		if state.Rated != play.rated || state.Bot.Name != play.name {
			t.Fatalf("%s, rated %t: state rated %t, bot %+v", play.bot, play.rated, state.Rated, state.Bot)
		}
		game, err := dal.GetGameByID(stack.database, state.GameID)
		if err != nil || game.Rated != play.rated {
			t.Fatalf("game = %+v, %v", game, err)
		}
		players, err := dal.LoadFrozenPlayers(stack.database, state.GameID)
		if err != nil || len(players) != 2 {
			t.Fatalf("frozen players = %+v, %v", players, err)
		}
		for _, player := range players {
			want := ""
			if play.rated && player.PlayerID == dana.playerID {
				want = play.competitor
			} else if play.rated {
				want = house.ID
			}
			if got := player.CompetitorID; (got == nil) != (want == "") || (got != nil && *got != want) {
				t.Fatalf("%s, rated %t: %s's competitor = %v, want %q", play.bot, play.rated, player.DisplayName, got, want)
			}
		}
	}
}

// The start hook refuses a rated roster that the ready gate would, before
// it binds a game, whoever sends it.
func TestARatedStartHookNeverStartsHalfRated(t *testing.T) {
	database := testutils.InitTestDB(t)
	snapshot := startedSnapshot()
	snapshot.Lobby.GameSettings = testutils.CircleMapSettings(2, `{"rated":true}`)
	account := uuid.NewString()
	snapshot.Members[0].AccountID = &account
	for index := range snapshot.Members {
		snapshot.Members[index].MemberSettings = []byte(`{"bot":{"kind":"manual"}}`)
	}
	if _, err := start(t, hooksClient(t, database, &testutils.RecordedPublisher{}), snapshot); err == nil {
		t.Fatal("a rated start with a guest started")
	}
	assertBindingCounts(t, database, snapshot.Lobby.ID, 0, 0)

	other := uuid.NewString()
	snapshot.Members[1].AccountID = &other
	gameID, err := start(t, hooksClient(t, database, &testutils.RecordedPublisher{}), snapshot)
	if err != nil {
		t.Fatal(err)
	}
	players, err := dal.LoadFrozenPlayers(database.WithContext(context.Background()), gameID)
	if err != nil || len(players) != 2 || players[0].CompetitorID == nil || players[1].CompetitorID == nil || *players[0].CompetitorID == *players[1].CompetitorID {
		t.Fatalf("frozen players = %+v, %v", players, err)
	}
}
