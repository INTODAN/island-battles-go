package lobbyadapter_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
	"time"

	"islandBattles/backend/dal"
	"islandBattles/backend/lobbyadapter"
	"islandBattles/backend/models"
	"islandBattles/backend/services"
	"islandBattles/lobby/domain"

	"github.com/google/uuid"
)

// fakeResolver answers from a table, so no test looks up a real name.
type fakeResolver map[string]string

func (resolver fakeResolver) LookupNetIP(_ context.Context, _ string, host string) ([]netip.Addr, error) {
	address, found := resolver[host]
	if !found {
		return nil, errors.New("no such host")
	}
	return []netip.Addr{netip.MustParseAddr(address)}, nil
}

var botHosts = fakeResolver{
	"bot.example.com":     "203.0.113.7",
	"seat.example.com":    "2001:db8::7",
	"private.example.com": "10.0.0.1",
	"loop.example.com":    "::1",
}

// newServerBotStack is a stack with ISLAND_BATTLES_SERVER_BOTS=on, whose
// hosted bots' hosts resolve through botHosts.
func newServerBotStack(t *testing.T) *lobbyStack {
	t.Helper()
	return newLobbyStackWith(t, services.ServerBots{Enabled: true, Resolver: botHosts})
}

// uploadPackage stores a ready package of owner in the lobby, as completing
// an upload does, replacing owner's previous one, and returns its ID.
func (stack *lobbyStack) uploadPackage(room domain.Snapshot, owner guest, ownerName, language string) string {
	stack.t.Helper()
	packageID := uuid.NewString()
	uploaded := time.Now().UTC().Truncate(time.Microsecond)
	record := models.BotPackage{PackageID: packageID, LobbyID: room.Lobby.ID, OwnerPlayerID: owner.playerID, OwnerName: ownerName, Status: models.BotPackagePending,
		S3Key: "lobbies/" + room.Lobby.ID + "/" + packageID + ".zip", CreatedAt: uploaded}
	if err := dal.CreateBotPackage(stack.database, record); err != nil {
		stack.t.Fatal(err)
	}
	record.Language, record.EntryOrCommand, record.SizeBytes, record.UploadedAt = language, "bot:play", 1234, &uploaded
	if _, completed, err := dal.CompleteBotPackage(stack.database, record); err != nil || !completed {
		stack.t.Fatalf("complete the package: %v, %v", completed, err)
	}
	return packageID
}

// teamsRoom is an Island Battles lobby with the named teams, which host
// created, on the first team.
func (stack *lobbyStack) teamsRoom(host guest, names ...string) domain.Snapshot {
	stack.t.Helper()
	teams := make([]map[string]any, len(names))
	for index, name := range names {
		teams[index] = map[string]any{"name": name}
	}
	var snapshot domain.Snapshot
	stack.expect(stack.api.Post("/lobby/v1/lobbies", host.header, map[string]any{
		"request_id": uuid.NewString(), "game_key": lobbyadapter.GameKey, "name": "Server bots", "teams": teams, "client_instance_id": host.client,
	}), http.StatusCreated, &snapshot)
	stack.expect(stack.api.Put(stack.lobbyPath(snapshot)+"/team", host.header, map[string]any{"team_id": snapshot.Teams[0].ID}), http.StatusOK, nil)
	return snapshot
}

// join adds member to the room, on the team at index team.
func (stack *lobbyStack) join(room domain.Snapshot, member guest, team int) {
	stack.t.Helper()
	stack.expect(stack.api.Post("/lobby/v1/memberships", member.header, map[string]any{"game_key": room.Lobby.GameKey, "join_code": room.Lobby.JoinCode, "client_instance_id": member.client}), http.StatusOK, nil)
	stack.expect(stack.api.Put(stack.lobbyPath(room)+"/team", member.header, map[string]any{"team_id": room.Teams[team].ID}), http.StatusOK, nil)
}

// addSeat asks, as host, for a bot seat with settings on the team at index
// team.
func (stack *lobbyStack) addSeat(room domain.Snapshot, host guest, team int, settings map[string]any) *httptest.ResponseRecorder {
	return stack.api.Post(stack.lobbyPath(room)+"/seats", host.header, map[string]any{"team_id": room.Teams[team].ID, "seat_settings": settings})
}

// seatOn is the seat on the team at index team of a snapshot.
func seatOn(t *testing.T, snapshot domain.Snapshot, team int) domain.Member {
	t.Helper()
	for _, member := range snapshot.Members {
		if member.IsSeat() && member.TeamID != nil && *member.TeamID == snapshot.Teams[team].ID {
			return member
		}
	}
	t.Fatalf("no seat on team %d: %+v", team, snapshot.Members)
	return domain.Member{}
}

type gameRequirement struct {
	Code   string `json:"code"`
	Type   string `json:"type"`
	Detail string `json:"detail"`
}

func (stack *lobbyStack) serverBots() bool {
	stack.t.Helper()
	var defaults struct {
		ServerBots *bool `json:"server_bots"`
	}
	stack.expect(stack.api.Get("/bots/defaults"), http.StatusOK, &defaults)
	if defaults.ServerBots == nil {
		stack.t.Fatal("GET /bots/defaults has no server_bots")
	}
	return *defaults.ServerBots
}

// With server bots off, the default, nothing about them is offered: their
// kinds and seats are refused as before, and GET /bots/defaults says so.
func TestServerBotsAreRefusedWhileTheFlagIsOff(t *testing.T) {
	stack := newLobbyStack(t)
	alice, bob := stack.guest("Alice"), stack.guest("Bob")
	room := stack.room(lobbyadapter.GameKey, alice, bob)
	shape := `member_settings must be {"bot": {"kind": "localhost"}} or {"bot": {"kind": "default", "level": N}} with a level that GET /bots/defaults lists`
	for settings, detail := range map[string]string{
		`{"bot":{"kind":"hosted","url":"https://bot.example.com/turn"}}`:      shape,
		`{"bot":{"kind":"uploaded","package_id":"` + uuid.NewString() + `"}}`: shape,
		`{"bot":{"kind":"hosted"}}`:                                           "kind must be localhost or default",
		`{"bot":{"kind":"uploaded"}}`:                                         "kind must be localhost or default",
	} {
		var refusal problem
		stack.expect(stack.setMemberSettings(room, alice, settings), http.StatusUnprocessableEntity, &refusal)
		if refusal.Code != "invalid_member_settings" || refusal.Detail != detail {
			t.Errorf("member settings %s = %+v", settings, refusal)
		}
	}
	carol := stack.guest("Carol")
	solo := stack.soloRoom(lobbyadapter.GameKey, carol)
	packageID := stack.uploadPackage(solo, carol, "Carol", "python")
	for _, settings := range []map[string]any{{"bot_url": "https://bot.example.com/turn"}, {"bot_package_id": packageID}} {
		var refusal problem
		stack.expect(stack.addSeat(solo, carol, 1, settings), http.StatusUnprocessableEntity, &refusal)
		if refusal.Code != "invalid_seat_settings" || refusal.Detail != `seat_settings must be {"bot_level": N} with a level that GET /bots/defaults lists: 1, 2, 3` {
			t.Errorf("seat settings %v = %+v", settings, refusal)
		}
	}
	if stack.serverBots() {
		t.Fatal("server_bots is true with the flag off")
	}
}

func TestAHostedBotURLFollowsEveryRule(t *testing.T) {
	stack := newServerBotStack(t)
	if !stack.serverBots() {
		t.Fatal("server_bots is false with the flag on")
	}
	alice, bob := stack.guest("Alice"), stack.guest("Bob")
	room := stack.room(lobbyadapter.GameKey, alice, bob)
	carol := stack.guest("Carol")
	solo := stack.soloRoom(lobbyadapter.GameKey, carol)
	refusals := map[string]string{
		"http://bot.example.com/turn":                          "must start with https://",
		"https://203.0.113.7/turn":                             "not an IP address",
		"https://[2001:db8::7]/turn":                           "not an IP address",
		"https://private.example.com/turn":                     "resolves to 10.0.0.1",
		"https://loop.example.com/turn":                        "resolves to ::1",
		"https://alex:secret@bot.example.com/turn":             "user name or password",
		"https://bot.example.com:8443/turn":                    "port 443",
		"https://unknown.example.com/turn":                     "could not be looked up",
		"https://bot.example.com/" + strings.Repeat("a", 2048): "longer than 2048 characters",
	}
	for url, reason := range refusals {
		var refusal problem
		stack.expect(stack.choose(room, alice, `{"kind":"hosted","url":"`+url+`"}`), http.StatusUnprocessableEntity, &refusal)
		if refusal.Code != "invalid_member_settings" || !strings.Contains(refusal.Detail, reason) {
			t.Errorf("hosted bot %q = %+v, want a refusal that says %q", url, refusal, reason)
		}
		// A seat's URL follows the same rules.
		var seatRefusal problem
		stack.expect(stack.addSeat(solo, carol, 1, map[string]any{"bot_url": url}), http.StatusUnprocessableEntity, &seatRefusal)
		if seatRefusal.Code != "invalid_seat_settings" || !strings.Contains(seatRefusal.Detail, reason) {
			t.Errorf("hosted bot seat %q = %+v, want a refusal that says %q", url, seatRefusal, reason)
		}
	}
	for settings, detail := range map[string]string{
		`{"bot":{"kind":"hosted"}}`: "kind hosted needs a url: where your bot server takes turns, such as https://bot.example.com/turn",
		`{"bot":{"kind":"hosted","url":"https://bot.example.com","level":1}}`: "level is only for kind default",
		`{"bot":{"kind":"localhost","url":"https://bot.example.com"}}`:        "url is only for kind hosted",
		`{"bot":{"kind":"uploaded"}}`:                                         "kind uploaded needs a package_id: one of your packages in this lobby",
		`{"bot":{"kind":"default","level":1,"package_id":"x"}}`:               "package_id is only for kind uploaded",
		`{"bot":{"kind":"remote"}}`:                                           "kind must be localhost, default, hosted or uploaded",
	} {
		var refusal problem
		stack.expect(stack.setMemberSettings(room, alice, settings), http.StatusUnprocessableEntity, &refusal)
		if refusal.Code != "invalid_member_settings" || refusal.Detail != detail {
			t.Errorf("member settings %s = %+v", settings, refusal)
		}
	}
	for _, settings := range []map[string]any{{"bot_url": "https://seat.example.com/turn", "bot_level": 1}, {"bot_url": ""}, {}} {
		var refusal problem
		stack.expect(stack.addSeat(solo, carol, 1, settings), http.StatusUnprocessableEntity, &refusal)
		if refusal.Code != "invalid_seat_settings" || !strings.Contains(refusal.Detail, `{"bot_url": "https://…"}`) {
			t.Errorf("seat settings %v = %+v", settings, refusal)
		}
	}

	var chosen domain.Snapshot
	stack.expect(stack.choose(room, alice, `{"kind":"hosted","url":"https://bot.example.com/turn?team=red"}`), http.StatusOK, &chosen)
	if bots := stack.bots(chosen); !sameBot(bots[alice.playerID], &models.BotChoiceView{Kind: models.HostedBot, Host: "bot.example.com"}) {
		t.Fatalf("Alice's bot = %+v", bots[alice.playerID])
	}
	stack.expect(stack.ready(room, alice), http.StatusOK, nil)
	var seated domain.Snapshot
	stack.expect(stack.addSeat(solo, carol, 1, map[string]any{"bot_url": "https://seat.example.com/turn"}), http.StatusOK, &seated)
	if seat := seatOn(t, seated, 1); seat.DisplayName != services.HostedBotSeatName || string(seat.SeatSettings) != `{"bot_url":"https://seat.example.com/turn"}` {
		t.Fatalf("hosted bot seat = %+v", seat)
	}
}

// An uploaded bot is a ready package of the lobby: your own for your member
// settings, anyone's for a seat. A package replaced or deleted after it was
// chosen fails the ready check, for a member and at the start for a seat.
// The start freezes every kind of bot.
func TestUploadedAndHostedBotsAreCheckedAndFrozenAtTheStart(t *testing.T) {
	stack := newServerBotStack(t)
	alice, bob := stack.guest("Alice"), stack.guest("Bob")
	room := stack.teamsRoom(alice, "Red", "Blue", "Green", "Yellow", "Purple")
	stack.join(room, bob, 1)
	alicePackage := stack.uploadPackage(room, alice, "Alice", "python")
	bobPackage := stack.uploadPackage(room, bob, "Bob", "native")

	// A package must be a ready one of this lobby.
	pending := uuid.NewString()
	if err := dal.CreateBotPackage(stack.database, models.BotPackage{PackageID: pending, LobbyID: room.Lobby.ID, OwnerPlayerID: alice.playerID, OwnerName: "Alice",
		Status: models.BotPackagePending, S3Key: "lobbies/" + room.Lobby.ID + "/" + pending + ".zip", CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	other := stack.teamsRoom(stack.guest("Dana"), "Red", "Blue")
	elsewhere := stack.uploadPackage(other, alice, "Alice", "python")
	for _, packageID := range []string{uuid.NewString(), "not-a-package", pending, elsewhere} {
		var refusal problem
		stack.expect(stack.choose(room, alice, `{"kind":"uploaded","package_id":"`+packageID+`"}`), http.StatusUnprocessableEntity, &refusal)
		if refusal.Code != "invalid_member_settings" || !strings.Contains(refusal.Detail, "no uploaded bot with this package_id") {
			t.Errorf("package %s = %+v", packageID, refusal)
		}
		var seatRefusal problem
		stack.expect(stack.addSeat(room, alice, 2, map[string]any{"bot_package_id": packageID}), http.StatusUnprocessableEntity, &seatRefusal)
		if seatRefusal.Code != "invalid_seat_settings" || !strings.Contains(seatRefusal.Detail, "no uploaded bot with this package_id") {
			t.Errorf("seat package %s = %+v", packageID, seatRefusal)
		}
	}

	// Your own bot must be your own package: Bob's is refused at ready.
	stack.expect(stack.choose(room, alice, `{"kind":"uploaded","package_id":"`+bobPackage+`"}`), http.StatusOK, nil)
	var refusal gameRequirement
	stack.expect(stack.ready(room, alice), http.StatusConflict, &refusal)
	if refusal.Code != "game_requirement" || refusal.Type != services.BotPackageGoneProblemType || !strings.Contains(refusal.Detail, "a package you uploaded") {
		t.Fatalf("ready with Bob's package = %+v", refusal)
	}
	stack.expect(stack.choose(room, alice, `{"kind":"uploaded","package_id":"`+alicePackage+`"}`), http.StatusOK, nil)
	stack.expect(stack.ready(room, alice), http.StatusOK, nil)
	stack.expect(stack.choose(room, bob, `{"kind":"hosted","url":"https://bot.example.com/turn/secret-path"}`), http.StatusOK, nil)
	stack.expect(stack.ready(room, bob), http.StatusOK, nil)

	// The host seats any member's package, and a hosted bot, and a default bot.
	var seated domain.Snapshot
	stack.expect(stack.addSeat(room, alice, 2, map[string]any{"bot_package_id": bobPackage}), http.StatusOK, &seated)
	if seat := seatOn(t, seated, 2); seat.DisplayName != "Bob's bot" {
		t.Fatalf("Bob's package's seat = %+v", seat)
	}
	stack.expect(stack.addSeat(room, alice, 3, map[string]any{"bot_url": "https://seat.example.com/turn"}), http.StatusOK, nil)
	stack.expect(stack.addSeat(room, alice, 4, map[string]any{"bot_level": 2}), http.StatusOK, nil)

	// Alice replaces her package. She is still ready, but the start refuses
	// her until she chooses it again, and so does getting ready.
	newAlicePackage := stack.uploadPackage(room, alice, "Alice", "python")
	start := func() *httptest.ResponseRecorder {
		return stack.api.Post(stack.lobbyPath(room)+"/start", alice.header, map[string]any{})
	}
	stack.expect(start(), http.StatusConflict, &refusal)
	if refusal.Type != services.BotPackageGoneProblemType || refusal.Detail != "The uploaded bot of Alice was replaced: they must choose it again." {
		t.Fatalf("start after Alice replaced her package = %+v", refusal)
	}
	stack.expect(stack.api.Put(stack.lobbyPath(room)+"/ready", alice.header, map[string]any{"ready": false}), http.StatusOK, nil)
	stack.expect(stack.ready(room, alice), http.StatusConflict, &refusal)
	if refusal.Type != services.BotPackageGoneProblemType || refusal.Detail != "Your uploaded bot was replaced; choose it again." {
		t.Fatalf("ready after Alice replaced her package = %+v", refusal)
	}
	stack.expect(stack.choose(room, alice, `{"kind":"uploaded","package_id":"`+newAlicePackage+`"}`), http.StatusOK, nil)
	stack.expect(stack.ready(room, alice), http.StatusOK, nil)

	// Bob deletes the package that the seat plays: the start refuses until
	// the host seats it again.
	if deleted, err := dal.DeleteBotPackage(stack.database, room.Lobby.ID, bobPackage); err != nil || !deleted {
		t.Fatalf("delete Bob's package: %v, %v", deleted, err)
	}
	stack.expect(start(), http.StatusConflict, &refusal)
	if refusal.Type != services.BotPackageGoneProblemType || refusal.Detail != "The uploaded bot of Bob's bot was replaced or deleted: remove the seat and add the bot again." {
		t.Fatalf("start after Bob deleted his package = %+v", refusal)
	}
	stack.expect(stack.api.Delete(stack.lobbyPath(room)+"/seats/"+seatOn(t, seated, 2).ID, alice.header), http.StatusOK, nil)
	newBobPackage := stack.uploadPackage(room, bob, "Bob", "native")
	var final domain.Snapshot
	stack.expect(stack.addSeat(room, alice, 2, map[string]any{"bot_package_id": newBobPackage}), http.StatusOK, &final)

	var started domain.Snapshot
	stack.expect(start(), http.StatusOK, &started)
	if started.Lobby.Phase != domain.PhaseStarted {
		t.Fatalf("phase = %s", started.Lobby.Phase)
	}

	// Each kind is frozen with what it needs.
	aliceState := stack.state(room, alice)
	players, err := dal.LoadFrozenPlayers(stack.database, aliceState.GameID)
	if err != nil || len(players) != 5 {
		t.Fatalf("frozen players = %+v, %v", players, err)
	}
	frozen := map[string]models.FrozenGamePlayer{}
	for _, player := range players {
		frozen[player.PlayerID] = player
	}
	value := func(pointer *string) string {
		if pointer == nil {
			return "<nil>"
		}
		return *pointer
	}
	check := func(playerID string, kind models.BotKind, level *uint, url, packageID, key, language string) {
		t.Helper()
		player := frozen[playerID]
		if player.BotKind != kind || (player.BotLevel == nil) != (level == nil) || (level != nil && *player.BotLevel != *level) ||
			value(player.BotURL) != url || value(player.BotPackageID) != packageID || value(player.BotPackageKey) != key || value(player.BotPackageLanguage) != language {
			t.Errorf("frozen %s = kind %s, level %v, url %s, package %s, key %s, language %s", player.DisplayName, player.BotKind, player.BotLevel,
				value(player.BotURL), value(player.BotPackageID), value(player.BotPackageKey), value(player.BotPackageLanguage))
		}
	}
	two := uint(2)
	check(alice.playerID, models.UploadedBot, nil, "<nil>", newAlicePackage, "lobbies/"+room.Lobby.ID+"/"+newAlicePackage+".zip", "python")
	check(bob.playerID, models.HostedBot, nil, "https://bot.example.com/turn/secret-path", "<nil>", "<nil>", "<nil>")
	check(seatOn(t, final, 2).PlayerID, models.UploadedBot, nil, "<nil>", newBobPackage, "lobbies/"+room.Lobby.ID+"/"+newBobPackage+".zip", "native")
	check(seatOn(t, final, 3).PlayerID, models.HostedBot, nil, "https://seat.example.com/turn", "<nil>", "<nil>", "<nil>")
	check(seatOn(t, final, 4).PlayerID, models.DefaultBot, &two, "<nil>", "<nil>", "<nil>", "<nil>")

	// GenerationState.bot reports the kind, and only the host of a hosted
	// bot's URL.
	if !sameBot(&aliceState.Bot, &models.BotChoiceView{Kind: models.UploadedBot}) || aliceState.Submitted {
		t.Fatalf("Alice's state = %+v", aliceState.Bot)
	}
	response := stack.api.Get("/game/from-lobby/"+room.Lobby.ID+"/generation", stack.playerToken(bob, lobbyadapter.GameKey))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"bot":{"kind":"hosted","host":"bot.example.com"}`) || strings.Contains(response.Body.String(), "secret-path") {
		t.Fatalf("Bob's state = %s", response.Body.String())
	}
}

// A long owner name is shortened to fit the lobby's seat names.
func TestUploadedBotSeatNamesFitTheLobby(t *testing.T) {
	for owner, want := range map[string]string{
		"Alex":                  "Alex's bot",
		"":                      "Uploaded bot",
		strings.Repeat("a", 34): strings.Repeat("a", 34) + "'s bot",
		strings.Repeat("b", 40): strings.Repeat("b", 33) + "…'s bot",
	} {
		if got := services.UploadedBotSeatName(owner); got != want || len([]rune(got)) > 40 {
			t.Errorf("UploadedBotSeatName(%q) = %q, want %q", owner, got, want)
		}
	}
}
