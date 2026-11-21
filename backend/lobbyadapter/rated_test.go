package lobbyadapter_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/INTODAN/mpl/domain"
	lobbypostgres "github.com/INTODAN/mpl/postgres"
	"islandBattles/backend/dal"
	"islandBattles/backend/internal/testutils"
	"islandBattles/backend/lobbyadapter"
	"islandBattles/backend/models"
	"islandBattles/backend/services"

	"github.com/google/uuid"
	"gorm.io/gorm"
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

// registerBotName registers a bot name for a signed-in player, with the
// hosted bot URL botURL(name).
func (stack *lobbyStack) registerBotName(member guest, name string) models.CompetitorView {
	stack.t.Helper()
	var competitor models.CompetitorView
	stack.expect(stack.api.Post("/account/bot-names", stack.playerToken(member, lobbyadapter.GameKey), map[string]string{"name": name, "url": botURL(name)}), http.StatusCreated, &competitor)
	return competitor
}

// botURL is the hosted bot URL that registerBotName gives a bot name.
func botURL(name string) string { return "https://bot.example.com/" + strings.ToLower(name) }

// withoutURL takes a bot name's URL away, as a name registered before names
// had URLs has none.
func (stack *lobbyStack) withoutURL(competitorID string) {
	stack.t.Helper()
	if err := stack.database.Model(&models.Competitor{}).Where(&models.Competitor{ID: competitorID}).Update("url", nil).Error; err != nil {
		stack.t.Fatal(err)
	}
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
// or with a hosted bot under one of their bot names, which has a URL: a
// named localhost or uploaded bot is refused.
func TestARatedLobbysReadyGate(t *testing.T) {
	stack := newServerBotStack(t)
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
	for _, bot := range []string{`{"kind":"default","level":2}`, `{"kind":"localhost"}`, `{"kind":"localhost","port":41415}`, `{"kind":"hosted","url":"https://bot.example.com/turn"}`} {
		stack.expect(stack.choose(room, dana, bot), http.StatusOK, nil)
		if detail := stack.expectRequirement(stack.ready(room, dana), services.RatedBotNameRequiredProblemType); detail != "This lobby is rated. Play by hand, or choose one of your hosted bot names." {
			t.Fatalf("%s = %q", bot, detail)
		}
	}
	// A named bot must be hosted: a localhost or an uploaded one is refused.
	danasPackage := stack.uploadPackage(room, dana, "Dana", "python")
	for _, bot := range []string{`{"kind":"localhost","name":"Kraken"}`, `{"kind":"uploaded","package_id":"` + danasPackage + `","name":"Kraken"}`} {
		stack.expect(stack.choose(room, dana, bot), http.StatusOK, nil)
		if detail := stack.expectRequirement(stack.ready(room, dana), services.RatedHostedBotRequiredProblemType); detail != "This lobby is rated. Rated bots run at their own URL: choose one of your hosted bot names, or play by hand." {
			t.Fatalf("%s = %q", bot, detail)
		}
	}
	// A hosted bot name of theirs, with no url, is compared without regard
	// to case and stored as given; a url beside the name is taken, and
	// ignored in a rated lobby. Playing by hand needs no name.
	for _, bot := range []string{`{"kind":"hosted","name":"kraken"}`, `{"kind":"hosted","name":"Kraken","url":"https://bot.example.com/elsewhere"}`, `{"kind":"manual"}`} {
		var chosen domain.Snapshot
		stack.expect(stack.choose(room, dana, bot), http.StatusOK, &chosen)
		if settings := string(memberOf(t, chosen, dana.playerID).MemberSettings); settings != `{"bot":`+bot+`}` {
			t.Fatalf("Dana's settings = %s", settings)
		}
		stack.expect(stack.ready(room, dana), http.StatusOK, nil)
	}

	// A name without a URL, registered before names had URLs, can't be
	// chosen without a url, and gets nobody ready in a rated lobby until its
	// owner adds one.
	leviathan := stack.registerBotName(dana, "Leviathan")
	stack.withoutURL(leviathan.ID)
	if detail := stack.expectInvalid(stack.choose(room, dana, `{"kind":"hosted","name":"leviathan"}`), "invalid_member_settings"); detail != "Leviathan has no URL yet: add its hosted bot's URL on the account page, or give your hosted bot a url." {
		t.Fatalf("a name without a URL = %q", detail)
	}
	stack.expect(stack.choose(room, dana, `{"kind":"hosted","url":"https://bot.example.com/turn","name":"Leviathan"}`), http.StatusOK, nil)
	if detail := stack.expectRequirement(stack.ready(room, dana), services.BotNameURLRequiredProblemType); detail != "Your bot name Leviathan has no URL yet. Add its hosted bot's URL on the account page, then get ready again." {
		t.Fatalf("a name without a URL = %q", detail)
	}
	stack.expect(stack.api.Put("/account/bot-names/"+leviathan.ID, stack.playerToken(dana, lobbyadapter.GameKey), map[string]string{"url": botURL("Leviathan")}), http.StatusOK, nil)
	stack.expect(stack.ready(room, dana), http.StatusOK, nil)

	// A name deleted after it was chosen no longer gets them ready.
	stack.expect(stack.choose(room, dana, `{"kind":"hosted","name":"Kraken"}`), http.StatusOK, nil)
	stack.expect(stack.api.Delete("/account/bot-names/"+kraken.ID, stack.playerToken(dana, lobbyadapter.GameKey)), http.StatusNoContent, nil)
	if detail := stack.expectRequirement(stack.ready(room, dana), services.RatedBotNameGoneProblemType); detail != "Your bot name Kraken no longer exists. Choose another." {
		t.Fatalf("a deleted name's refusal = %q", detail)
	}
}

// A bot's name must be one of the member's own, in every lobby; a guest has
// none, and only a bot that a client or the server runs takes one.
func TestABotNameMustBeOneOfYours(t *testing.T) {
	stack := newServerBotStack(t)
	dana, erin, guestPlayer := stack.player("google-dana", "Dana"), stack.player("google-erin", "Erin"), stack.guest("Gus")
	stack.registerBotName(dana, "Kraken")
	for _, rated := range []bool{true, false} {
		room := stack.lobbyOf(dana, 3, ratedSettings(3, rated))
		stack.join(room, erin, 1)
		stack.join(room, guestPlayer, 2)
		for _, member := range []guest{erin, guestPlayer} {
			for _, bot := range []string{`{"kind":"localhost","name":"Kraken"}`, `{"kind":"hosted","name":"Kraken"}`} {
				if detail := stack.expectInvalid(stack.choose(room, member, bot), "invalid_member_settings"); detail != "Kraken isn't one of your bot names." {
					t.Fatalf("rated %t: another's name = %q", rated, detail)
				}
			}
		}
		for _, bot := range []string{`{"kind":"manual","name":"Kraken"}`, `{"kind":"default","level":2,"name":"Kraken"}`, `{"kind":"hosted"}`} {
			stack.expectInvalid(stack.choose(room, dana, bot), "invalid_member_settings")
		}
		stack.expect(stack.choose(room, dana, `{"kind":"hosted","name":"KRAKEN"}`), http.StatusOK, nil)
		stack.expect(stack.ready(room, dana), http.StatusOK, nil)
	}
}

// A lobby that isn't rated takes every kind of bot, named or not, as before
// rated bots were hosted only. A named hosted bot without a url plays at its
// name's URL, so it needs the name to exist still.
func TestAnUnratedLobbyTakesEveryNamedBot(t *testing.T) {
	stack := newServerBotStack(t)
	dana := stack.player("google-dana", "Dana")
	kraken := stack.registerBotName(dana, "Kraken")
	room := stack.lobbyOf(dana, 2, ratedSettings(2, false))
	danasPackage := stack.uploadPackage(room, dana, "Dana", "python")
	for _, bot := range []string{
		`{"kind":"manual"}`, `{"kind":"default","level":2}`, `{"kind":"localhost"}`, `{"kind":"localhost","name":"Kraken"}`,
		`{"kind":"uploaded","package_id":"` + danasPackage + `"}`, `{"kind":"uploaded","name":"Kraken","package_id":"` + danasPackage + `"}`,
		`{"kind":"hosted","url":"https://bot.example.com/turn"}`, `{"kind":"hosted","name":"Kraken","url":"https://bot.example.com/turn"}`, `{"kind":"hosted","name":"Kraken"}`,
	} {
		stack.expect(stack.choose(room, dana, bot), http.StatusOK, nil)
		stack.expect(stack.ready(room, dana), http.StatusOK, nil)
	}
	stack.expect(stack.choose(room, dana, `{"kind":"hosted","name":"KRAKEN"}`), http.StatusOK, nil)
	stack.expect(stack.api.Delete("/account/bot-names/"+kraken.ID, stack.playerToken(dana, lobbyadapter.GameKey)), http.StatusNoContent, nil)
	if detail := stack.expectRequirement(stack.ready(room, dana), services.RatedBotNameGoneProblemType); detail != "Your bot name KRAKEN no longer exists. Choose another." {
		t.Fatalf("a deleted name's refusal = %q", detail)
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
// since, or without a URL, and a seat added before.
func TestARatedStartChecksTheRosterAgain(t *testing.T) {
	stack := newServerBotStack(t)
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
	if detail := stack.expectRequirement(stack.start(room, dana), services.RatedBotNameRequiredProblemType); detail != "This lobby is rated: waiting for Erin to play by hand, or to choose one of their hosted bot names." {
		t.Fatalf("a default bot at the start = %q", detail)
	}

	room = readyRoom(false, map[guest]string{dana: `{"kind":"manual"}`, erin: `{"kind":"localhost","name":"Kraken"}`})
	stack.setRated(room, true)
	if detail := stack.expectRequirement(stack.start(room, dana), services.RatedHostedBotRequiredProblemType); detail != "This lobby is rated, and rated bots run at their own URL: waiting for Erin to choose one of their hosted bot names, or to play by hand." {
		t.Fatalf("a named localhost bot at the start = %q", detail)
	}

	room = readyRoom(false, map[guest]string{dana: `{"kind":"manual"}`, erin: `{"kind":"hosted","url":"https://bot.example.com/turn","name":"Kraken"}`})
	stack.withoutURL(kraken.ID)
	stack.setRated(room, true)
	if detail := stack.expectRequirement(stack.start(room, dana), services.BotNameURLRequiredProblemType); detail != "Erin's bot name Kraken has no URL yet: they must add its hosted bot's URL on the account page." {
		t.Fatalf("a name without a URL at the start = %q", detail)
	}
	stack.expect(stack.api.Put("/account/bot-names/"+kraken.ID, stack.playerToken(erin, lobbyadapter.GameKey), map[string]string{"url": botURL("Kraken")}), http.StatusOK, nil)

	room = readyRoom(true, map[guest]string{dana: `{"kind":"manual"}`, erin: `{"kind":"hosted","name":"Kraken"}`})
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
// name, and a seat's house bot, and plays a named hosted bot at the name's
// URL as it was at the start. The same lobby unrated records none, and plays
// a hosted bot at its own url, or at its name's when it has none.
func TestARatedGameRecordsItsCompetitors(t *testing.T) {
	stack := newServerBotStack(t)
	dana := stack.player("google-dana", "Dana")
	kraken := stack.registerBotName(dana, "Kraken")
	var account models.IslandAccountView
	stack.expect(stack.api.Get("/account", stack.playerToken(dana, lobbyadapter.GameKey)), http.StatusOK, &account)
	house, err := dal.FindOrCreateHouseCompetitor(stack.database, 2)
	if err != nil {
		t.Fatal(err)
	}
	const ownURL = "https://bot.example.com/own"
	for _, play := range []struct {
		bot        string
		rated      bool
		competitor string
		name       string
		url        string
	}{
		{bot: `{"kind":"manual"}`, rated: true, competitor: account.Hand.ID},
		{bot: `{"kind":"hosted","name":"kraken"}`, rated: true, competitor: kraken.ID, name: "Kraken", url: botURL("Kraken")},
		{bot: `{"kind":"hosted","url":"` + ownURL + `","name":"Kraken"}`, rated: true, competitor: kraken.ID, name: "Kraken", url: botURL("Kraken")},
		{bot: `{"kind":"manual"}`, rated: false},
		{bot: `{"kind":"localhost","name":"Kraken"}`, rated: false},
		{bot: `{"kind":"hosted","name":"Kraken"}`, rated: false, url: botURL("Kraken")},
		{bot: `{"kind":"hosted","url":"` + ownURL + `","name":"Kraken"}`, rated: false, url: ownURL},
	} {
		room := stack.lobbyOf(dana, 2, ratedSettings(2, play.rated))
		stack.expect(stack.addSeat(room, dana, 1, map[string]any{"bot_level": 2}), http.StatusOK, nil)
		stack.expect(stack.choose(room, dana, play.bot), http.StatusOK, nil)
		stack.expect(stack.ready(room, dana), http.StatusOK, nil)
		stack.expect(stack.start(room, dana), http.StatusOK, nil)
		// A URL changed during the game doesn't change the game's.
		stack.expect(stack.api.Put("/account/bot-names/"+kraken.ID, stack.playerToken(dana, lobbyadapter.GameKey), map[string]string{"url": "https://bot.example.com/changed"}), http.StatusOK, nil)

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
			if player.PlayerID == dana.playerID {
				if got := player.BotURL; (got == nil) != (play.url == "") || (got != nil && *got != play.url) {
					t.Fatalf("%s, rated %t: Dana's bot URL = %v, want %q", play.bot, play.rated, got, play.url)
				}
			}
		}
		stack.expect(stack.api.Put("/account/bot-names/"+kraken.ID, stack.playerToken(dana, lobbyadapter.GameKey), map[string]string{"url": botURL("Kraken")}), http.StatusOK, nil)
	}
}

// "Play against it": anyone may add a listed bot's URL, from the
// leaderboard, as a hosted bot seat of a lobby of their own. A rated lobby
// refuses such a seat, so the game is unrated: the seat is no competitor,
// and the bot name's rating can't change.
func TestPlayingAgainstAListedBotIsUnrated(t *testing.T) {
	stack := newServerBotStack(t)
	dana, erin := stack.player("google-dana", "Dana"), stack.player("google-erin", "Erin")
	kraken := stack.registerBotName(dana, "Kraken")
	// Kraken is listed, with its URL, once it has played a rated game.
	if err := stack.database.Model(&models.Competitor{}).Where(&models.Competitor{ID: kraken.ID}).
		Updates(map[string]any{"games": 1, "wins": 1, "rating": 1520, "last_played_at": time.Now()}).Error; err != nil {
		t.Fatal(err)
	}
	var listed models.LeaderboardCompetitorView
	stack.expect(stack.api.Get("/leaderboard/"+kraken.ID), http.StatusOK, &listed)
	if listed.URL == nil || *listed.URL != botURL("Kraken") {
		t.Fatalf("Kraken's page = %+v", listed)
	}

	rated := stack.lobbyOf(erin, 2, ratedSettings(2, true))
	if detail := stack.expectInvalid(stack.addSeat(rated, erin, 1, map[string]any{"bot_url": *listed.URL}), "invalid_seat_settings"); detail != services.RatedSeatRule {
		t.Fatalf("Kraken's seat in a rated lobby = %q", detail)
	}
	room := stack.lobbyOf(erin, 2, ratedSettings(2, false))
	stack.expect(stack.addSeat(room, erin, 1, map[string]any{"bot_url": *listed.URL}), http.StatusOK, nil)
	stack.expect(stack.choose(room, erin, `{"kind":"manual"}`), http.StatusOK, nil)
	stack.expect(stack.ready(room, erin), http.StatusOK, nil)
	stack.expect(stack.start(room, erin), http.StatusOK, nil)

	state := stack.state(room, erin)
	game, err := dal.GetGameByID(stack.database, state.GameID)
	if err != nil || state.Rated || game.Rated {
		t.Fatalf("game = %+v, %v; state rated %t", game, err, state.Rated)
	}
	players, err := dal.LoadFrozenPlayers(stack.database, state.GameID)
	if err != nil || len(players) != 2 {
		t.Fatalf("frozen players = %+v, %v", players, err)
	}
	for _, player := range players {
		if player.CompetitorID != nil || player.BotName != nil {
			t.Fatalf("frozen player = %+v, want no competitor", player)
		}
		if player.PlayerID != erin.playerID && (player.BotKind != models.HostedBot || player.BotURL == nil || *player.BotURL != botURL("Kraken")) {
			t.Fatalf("Kraken's seat = %+v", player)
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

// A retried start of a rated round whose game has started returns that game,
// even when a roster member left the lobby, or deleted their bot name, since.
// The start never changes the display name of an account the game knows.
func TestARetriedRatedStartReturnsTheSameGame(t *testing.T) {
	database := testutils.InitTestDB(t)
	snapshot := startedSnapshot()
	snapshot.Lobby.GameSettings = testutils.CircleMapSettings(2, `{"rated":true}`)
	host, other := uuid.NewString(), uuid.NewString()
	snapshot.Members[0].AccountID, snapshot.Members[1].AccountID = &host, &other
	snapshot.Members[0].MemberSettings = []byte(`{"bot":{"kind":"manual"}}`)
	snapshot.Members[1].MemberSettings = []byte(`{"bot":{"kind":"hosted","name":"Kraken"}}`)
	var kraken models.Competitor
	if err := database.Transaction(func(transaction *gorm.DB) error {
		if err := dal.SeeAccount(transaction, host, "Host, renamed"); err != nil {
			return err
		}
		if err := dal.SeeAccount(transaction, other, "Guest"); err != nil {
			return err
		}
		var err error
		kraken, err = dal.AddBotName(transaction, other, "Kraken", botURL("Kraken"))
		return err
	}); err != nil {
		t.Fatal(err)
	}
	client := hooksClient(t, database, &testutils.RecordedPublisher{})
	gameID, err := start(t, client, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	var stored models.IslandAccount
	if err := database.Where(&models.IslandAccount{AccountID: host}).First(&stored).Error; err != nil || stored.DisplayName != "Host, renamed" {
		t.Fatalf("the host's account after the start = %+v, %v", stored, err)
	}

	if deleted, err := dal.DeleteBotName(database, other, kraken.ID); err != nil || !deleted {
		t.Fatalf("delete Kraken: %v, %v", deleted, err)
	}
	retried, err := start(t, client, snapshot)
	if err != nil || retried != gameID {
		t.Fatalf("a retry after Kraken was deleted = %d, %v; want game %d", retried, err, gameID)
	}
	// The lobby's snapshot no longer lists a member who left.
	snapshot.Members = snapshot.Members[:1]
	retried, err = start(t, client, snapshot)
	if err != nil || retried != gameID {
		t.Fatalf("a retry after a member left = %d, %v; want game %d", retried, err, gameID)
	}
	assertBindingCounts(t, database, snapshot.Lobby.ID, 1, 2)
	players, err := dal.LoadFrozenPlayers(database, gameID)
	if err != nil || len(players) != 2 {
		t.Fatalf("frozen players = %+v, %v", players, err)
	}
	for _, player := range players {
		if player.CompetitorID == nil || (player.PlayerID == snapshot.StartRoster[1].PlayerID && (*player.CompetitorID != kraken.ID || player.BotName == nil || *player.BotName != "Kraken")) {
			t.Fatalf("frozen player = %+v", player)
		}
	}
}

// One person against a level 2 bot seat in a rated lobby: the game changes
// both the person's hand play's rating and the house bot's, and its outcome
// says so.
func TestARatedGameAgainstABotSeatRatesThePersonAndTheHouseBot(t *testing.T) {
	stack := newLobbyStack(t)
	dana := stack.player("google-dana", "Dana")
	var account models.IslandAccountView
	stack.expect(stack.api.Get("/account", stack.playerToken(dana, lobbyadapter.GameKey)), http.StatusOK, &account)
	room := stack.lobbyOf(dana, 2, ratedSettings(2, true))
	stack.expect(stack.addSeat(room, dana, 1, map[string]any{"bot_level": 2}), http.StatusOK, nil)
	stack.expect(stack.choose(room, dana, `{"kind":"manual"}`), http.StatusOK, nil)
	stack.expect(stack.ready(room, dana), http.StatusOK, nil)
	stack.expect(stack.start(room, dana), http.StatusOK, nil)
	house, err := dal.FindOrCreateHouseCompetitor(stack.database, 2)
	if err != nil {
		t.Fatal(err)
	}

	// Dana resigns. The bot completed the turn at once, so it plays in the
	// resign request, and the bot's team wins.
	var ended models.GenerationState
	stack.expect(stack.api.Post("/game/from-lobby/"+room.Lobby.ID+"/resign", stack.playerToken(dana, lobbyadapter.GameKey), map[string]any{}), http.StatusOK, &ended)
	if ended.Outcome == nil || ended.Outcome.WinnerTeamColor == nil || *ended.Outcome.WinnerTeamColor == ended.TeamColor {
		t.Fatalf("outcome = %+v, want the bot's team to win", ended.Outcome)
	}
	houseColor := *ended.Outcome.WinnerTeamColor
	want := []models.RatingChange{
		{TeamColor: ended.TeamColor, CompetitorID: account.Hand.ID, Kind: models.ManualCompetitor, Name: "Dana", Before: 1500, After: 1480},
		{TeamColor: houseColor, CompetitorID: house.ID, Kind: models.HouseCompetitor, Name: "Bot (level 2)", Before: 1500, After: 1520},
	}
	if houseColor < ended.TeamColor {
		want[0], want[1] = want[1], want[0]
	}
	if !reflect.DeepEqual(ended.Outcome.Ratings, want) {
		t.Fatalf("ratings = %+v, want %+v", ended.Outcome.Ratings, want)
	}
	for id, rating := range map[string]float64{account.Hand.ID: 1480, house.ID: 1520} {
		var competitor models.Competitor
		if err := stack.database.Where(&models.Competitor{ID: id}).First(&competitor).Error; err != nil {
			t.Fatal(err)
		}
		if competitor.Rating != rating || competitor.Games != 1 {
			t.Fatalf("competitor after the game = %+v, want rating %g after one game", competitor, rating)
		}
	}
	// The account page shows the hand play's new rating.
	stack.expect(stack.api.Get("/account", stack.playerToken(dana, lobbyadapter.GameKey)), http.StatusOK, &account)
	if account.Hand.Rating != 1480 || account.Hand.Losses != 1 {
		t.Fatalf("hand play = %+v", account.Hand)
	}
}
