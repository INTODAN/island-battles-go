package lobbyadapter_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"islandBattles/backend/handlers"
	"islandBattles/backend/internal/testutils"
	"islandBattles/backend/lobbyadapter"
	"islandBattles/backend/models"
	"islandBattles/backend/routes"
	"islandBattles/backend/services"
	"islandBattles/lobby/application"
	"islandBattles/lobby/auth"
	"islandBattles/lobby/domain"
	lobbypostgres "islandBattles/lobby/postgres"

	"github.com/danielgtaylor/huma/v2/humatest"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// lobbyStack serves the lobby API and the Island Battles routes on one
// database, as the backend does, without Centrifugo.
type lobbyStack struct {
	t        *testing.T
	database *gorm.DB
	api      humatest.TestAPI
}

func newLobbyStack(t *testing.T) *lobbyStack {
	t.Helper()
	database := testutils.InitTestDB(t)
	clock := domain.SystemClock{}
	identifiers := domain.CryptoIdentifiers{}
	policy := domain.DefaultPolicy(lobbyadapter.Registration(database),
		domain.GameRegistration{Key: "quiz-demo", ValidateSettings: func(context.Context, string, json.RawMessage, []domain.TeamDefinition) error { return nil }})
	store, err := lobbypostgres.NewStore(database, clock)
	if err != nil {
		t.Fatal(err)
	}
	identities, err := lobbypostgres.NewIdentityStore(database)
	if err != nil {
		t.Fatal(err)
	}
	authentication, err := auth.NewService(identities, clock, policy, identifiers, []byte(strings.Repeat("s", 32)), "ws://127.0.0.1:1/connection/websocket")
	if err != nil {
		t.Fatal(err)
	}
	lobbies, err := application.NewService(store, clock, identifiers, policy)
	if err != nil {
		t.Fatal(err)
	}
	publisher := &testutils.RecordedPublisher{}
	binding, err := lobbyadapter.NewService(database, lobbies, publisher)
	if err != nil {
		t.Fatal(err)
	}
	_, api := humatest.New(t, routes.NewAPIConfig("test", "1.0.0"))
	routes.SetupLobbyRoutes(api, lobbies, authentication, binding, nil, &handlers.LobbyGame{Database: database, Authenticator: authentication, Publisher: publisher})
	return &lobbyStack{t: t, database: database, api: api}
}

// guest is a player with a lobby session.
type guest struct {
	playerID, header, client string
}

func (stack *lobbyStack) guest(name string) guest {
	stack.t.Helper()
	var session domain.GuestSession
	stack.expect(stack.api.Post("/lobby/v1/sessions", map[string]string{"display_name": name}), http.StatusCreated, &session)
	return guest{playerID: session.PlayerID, header: "Authorization: Bearer " + session.AccessToken, client: uuid.NewString()}
}

// expect checks a response's status and decodes its body into into, unless
// into is nil.
func (stack *lobbyStack) expect(response *httptest.ResponseRecorder, status int, into any) {
	stack.t.Helper()
	if response.Code != status {
		stack.t.Fatalf("status %d, want %d: %s", response.Code, status, response.Body.String())
	}
	if into != nil {
		if err := json.Unmarshal(response.Body.Bytes(), into); err != nil {
			stack.t.Fatal(err)
		}
	}
}

// room is a two-team lobby of gameKey that host created and other joined,
// each on their own team.
func (stack *lobbyStack) room(gameKey string, host, other guest) domain.Snapshot {
	stack.t.Helper()
	var snapshot domain.Snapshot
	stack.expect(stack.api.Post("/lobby/v1/lobbies", host.header, map[string]any{
		"request_id": uuid.NewString(), "game_key": gameKey, "name": "Bots",
		"teams": []map[string]any{{"name": "Red"}, {"name": "Blue"}}, "client_instance_id": host.client,
	}), http.StatusCreated, &snapshot)
	stack.expect(stack.api.Post("/lobby/v1/memberships", other.header, map[string]any{"game_key": gameKey, "join_code": snapshot.Lobby.JoinCode, "client_instance_id": other.client}), http.StatusOK, nil)
	for index, member := range []guest{host, other} {
		stack.expect(stack.api.Put(stack.lobbyPath(snapshot)+"/team", member.header, map[string]any{"team_id": snapshot.Teams[index].ID}), http.StatusOK, nil)
	}
	return snapshot
}

func (stack *lobbyStack) lobbyPath(snapshot domain.Snapshot) string {
	return fmt.Sprintf("/lobby/v1/games/%s/lobbies/%s", snapshot.Lobby.GameKey, snapshot.Lobby.ID)
}

func (stack *lobbyStack) ready(snapshot domain.Snapshot, member guest) *httptest.ResponseRecorder {
	return stack.api.Put(stack.lobbyPath(snapshot)+"/ready", member.header, map[string]any{"ready": true})
}

// choose sets member's bot, a JSON object, in their member settings.
func (stack *lobbyStack) choose(snapshot domain.Snapshot, member guest, bot string) *httptest.ResponseRecorder {
	return stack.setMemberSettings(snapshot, member, `{"bot":`+bot+`}`)
}

func (stack *lobbyStack) setMemberSettings(snapshot domain.Snapshot, member guest, settings string) *httptest.ResponseRecorder {
	return stack.api.Put(stack.lobbyPath(snapshot)+"/member-settings", member.header, strings.NewReader(`{"member_settings":`+settings+`}`))
}

// snapshot is the lobby as member reads it.
func (stack *lobbyStack) snapshot(room domain.Snapshot, member guest) domain.Snapshot {
	stack.t.Helper()
	var snapshot domain.Snapshot
	stack.expect(stack.api.Get(stack.lobbyPath(room), member.header), http.StatusOK, &snapshot)
	return snapshot
}

// bots are the bots in the members' member settings, by player ID; a member
// without one is absent.
func (stack *lobbyStack) bots(snapshot domain.Snapshot) map[string]*models.BotChoiceView {
	stack.t.Helper()
	bots := map[string]*models.BotChoiceView{}
	for _, member := range snapshot.Members {
		// A JSON null decodes into the raw bytes null.
		if member.MemberSettings == nil || string(member.MemberSettings) == "null" {
			continue
		}
		bot, err := services.MemberBot(member.MemberSettings)
		if err != nil {
			stack.t.Fatalf("%s's member settings %s: %v", member.DisplayName, member.MemberSettings, err)
		}
		bots[member.PlayerID] = &bot
	}
	return bots
}

func (stack *lobbyStack) state(snapshot domain.Snapshot, member guest) models.GenerationState {
	stack.t.Helper()
	var state models.GenerationState
	stack.expect(stack.api.Get("/game/from-lobby/"+snapshot.Lobby.ID+"/generation", member.header), http.StatusOK, &state)
	return state
}

func botChoice(kind models.BotKind, level uint) *models.BotChoiceView {
	if kind == models.LocalhostBot {
		return &models.BotChoiceView{Kind: kind}
	}
	return &models.BotChoiceView{Kind: kind, Level: &level}
}

func sameBot(got, want *models.BotChoiceView) bool {
	if got == nil || want == nil {
		return got == want
	}
	return got.Kind == want.Kind && (got.Level == nil) == (want.Level == nil) && (got.Level == nil || *got.Level == *want.Level)
}

func TestEveryMemberChoosesABotBeforeGettingReady(t *testing.T) {
	stack := newLobbyStack(t)
	alice, bob, outsider := stack.guest("Alice"), stack.guest("Bob"), stack.guest("Outsider")
	room := stack.room(lobbyadapter.GameKey, alice, bob)

	// Without a choice, the lobby refuses with the game's problem type.
	var refusal struct {
		Code   string `json:"code"`
		Type   string `json:"type"`
		Detail string `json:"detail"`
	}
	stack.expect(stack.ready(room, alice), http.StatusConflict, &refusal)
	if refusal.Code != "game_requirement" || refusal.Type != services.BotNotChosenProblemType || !strings.Contains(refusal.Detail, "Choose your bot") {
		t.Fatalf("ready without a bot = %+v", refusal)
	}
	if bots := stack.bots(stack.snapshot(room, bob)); len(bots) != 0 {
		t.Fatalf("bots before anyone chose = %+v", bots)
	}

	// The game checks the settings: invalid bots, other keys and settings
	// without a bot are invalid_member_settings with the game's detail.
	invalid := map[string]string{
		`{"bot":{"kind":"default"}}`:             "kind default needs a level that GET /bots/defaults lists: 1, 2, 3",
		`{"bot":{"kind":"default","level":4}}`:   "kind default needs a level that GET /bots/defaults lists: 1, 2, 3",
		`{"bot":{"kind":"default","level":0}}`:   "kind default needs a level that GET /bots/defaults lists: 1, 2, 3",
		`{"bot":{"kind":"localhost","level":1}}`: "level is only for kind default",
		`{"bot":{"kind":"uploaded"}}`:            "kind must be localhost or default",
		`{"bot":{"kind":"localhost","x":1}}`:     `member_settings must be {"bot": {"kind": "localhost"}} or {"bot": {"kind": "default", "level": N}} with a level that GET /bots/defaults lists`,
		`{"bot":{"kind":"localhost"},"x":1}`:     `member_settings must be {"bot": {"kind": "localhost"}} or {"bot": {"kind": "default", "level": N}} with a level that GET /bots/defaults lists`,
		`{}`:                                     `member_settings must be {"bot": {"kind": "localhost"}} or {"bot": {"kind": "default", "level": N}} with a level that GET /bots/defaults lists`,
	}
	for settings, detail := range invalid {
		var problem struct {
			Code   string `json:"code"`
			Detail string `json:"detail"`
		}
		stack.expect(stack.setMemberSettings(room, alice, settings), http.StatusUnprocessableEntity, &problem)
		if problem.Code != "invalid_member_settings" || problem.Detail != detail {
			t.Errorf("member settings %s = %+v", settings, problem)
		}
	}
	stack.expect(stack.choose(room, outsider, `{"kind":"localhost"}`), http.StatusNotFound, nil)
	stack.expect(stack.api.Put(stack.lobbyPath(room)+"/member-settings", strings.NewReader(`{"member_settings":{"bot":{"kind":"localhost"}}}`)), http.StatusUnauthorized, nil)

	// Each kind of bot is accepted, every member sees every member's bot, and
	// a bot can change while the lobby waits.
	var chosen domain.Snapshot
	stack.expect(stack.choose(room, alice, `{"kind":"localhost"}`), http.StatusOK, &chosen)
	if bots := stack.bots(chosen); len(bots) != 1 || !sameBot(bots[alice.playerID], botChoice(models.LocalhostBot, 0)) {
		t.Fatalf("bots after Alice chose = %+v", bots)
	}
	stack.expect(stack.ready(room, alice), http.StatusOK, nil)
	for level := uint(1); level <= 3; level++ {
		stack.expect(stack.choose(room, bob, fmt.Sprintf(`{"kind":"default","level":%d}`, level)), http.StatusOK, nil)
		if bots := stack.bots(stack.snapshot(room, alice)); !sameBot(bots[bob.playerID], botChoice(models.DefaultBot, level)) {
			t.Fatalf("Bob's choice of level %d as Alice sees it = %+v", level, bots[bob.playerID])
		}
	}
	stack.expect(stack.ready(room, bob), http.StatusOK, nil)

	// Choosing the same bot again keeps Bob ready; another bot clears his
	// ready, so the start sees the bot he confirmed.
	var same, changed domain.Snapshot
	stack.expect(stack.choose(room, bob, `{"level":3,"kind":"default"}`), http.StatusOK, &same)
	if !memberOf(t, same, bob.playerID).Ready {
		t.Fatal("choosing the same bot again cleared Bob's ready")
	}
	stack.expect(stack.choose(room, bob, `{"kind":"default","level":2}`), http.StatusOK, &changed)
	if memberOf(t, changed, bob.playerID).Ready || !memberOf(t, changed, alice.playerID).Ready || changed.Lobby.Version != same.Lobby.Version+1 {
		t.Fatalf("after Bob changed his bot: Bob ready %v, Alice ready %v, version %d after %d", memberOf(t, changed, bob.playerID).Ready, memberOf(t, changed, alice.playerID).Ready, changed.Lobby.Version, same.Lobby.Version)
	}
	stack.expect(stack.ready(room, bob), http.StatusOK, nil)

	var started domain.Snapshot
	stack.expect(stack.api.Post(stack.lobbyPath(room)+"/start", alice.header, map[string]any{}), http.StatusOK, &started)
	if started.Lobby.Phase != domain.PhaseStarted {
		t.Fatalf("lobby phase = %s", started.Lobby.Phase)
	}
	// Once the lobby starts, the game has its own copy, and choosing conflicts.
	stack.expect(stack.choose(room, bob, `{"kind":"default","level":3}`), http.StatusConflict, nil)
	stack.expect(stack.choose(room, alice, `{"kind":"default","level":1}`), http.StatusConflict, nil)
	// A refused choice is not stored.
	if bots := stack.bots(stack.snapshot(room, alice)); !sameBot(bots[bob.playerID], botChoice(models.DefaultBot, 2)) || !sameBot(bots[alice.playerID], botChoice(models.LocalhostBot, 0)) {
		t.Fatalf("bots after refused choices = %+v", bots)
	}
	assertFrozenBots := func() {
		t.Helper()
		aliceState, bobState := stack.state(room, alice), stack.state(room, bob)
		if !sameBot(&aliceState.Bot, botChoice(models.LocalhostBot, 0)) || !sameBot(&bobState.Bot, botChoice(models.DefaultBot, 2)) {
			t.Fatalf("frozen bots = %+v and %+v", aliceState.Bot, bobState.Bot)
		}
		// The backend played Bob's level 2 bot; Alice's own client plays hers.
		if aliceState.Submitted || !bobState.Submitted {
			t.Fatalf("submitted = %t and %t", aliceState.Submitted, bobState.Submitted)
		}
	}
	assertFrozenBots()

	// Deleting the lobby, as lobby retention does, deletes the member
	// settings with it, and the game keeps its copy.
	deleteLobby(t, stack.database, room.Lobby.ID)
	assertFrozenBots()
}

func memberOf(t *testing.T, snapshot domain.Snapshot, playerID string) domain.Member {
	t.Helper()
	for _, member := range snapshot.Members {
		if member.PlayerID == playerID {
			return member
		}
	}
	t.Fatalf("no member %s", playerID)
	return domain.Member{}
}

// The lobby checks the start roster's bots again at the start, from the
// members' settings, and names the members without one.
func TestStartRefusalNamesTheMembersWithoutABot(t *testing.T) {
	stack := newLobbyStack(t)
	alice, bob := stack.guest("Alice"), stack.guest("Bob")
	room := stack.room(lobbyadapter.GameKey, alice, bob)
	stack.expect(stack.choose(room, alice, `{"kind":"localhost"}`), http.StatusOK, nil)
	stack.expect(stack.choose(room, bob, `{"kind":"localhost"}`), http.StatusOK, nil)
	stack.expect(stack.ready(room, alice), http.StatusOK, nil)
	stack.expect(stack.ready(room, bob), http.StatusOK, nil)
	// Bob got ready before bots were required, which the API can't
	// reproduce, so remove his settings directly.
	if err := stack.database.Model(&lobbypostgres.MembershipRecord{}).Where(&lobbypostgres.MembershipRecord{LobbyID: room.Lobby.ID, PlayerID: bob.playerID}).Update("member_settings", nil).Error; err != nil {
		t.Fatal(err)
	}
	var refusal struct {
		Code   string `json:"code"`
		Type   string `json:"type"`
		Detail string `json:"detail"`
	}
	stack.expect(stack.api.Post(stack.lobbyPath(room)+"/start", alice.header, map[string]any{}), http.StatusConflict, &refusal)
	if refusal.Code != "game_requirement" || refusal.Type != services.BotNotChosenProblemType || !strings.Contains(refusal.Detail, "Bob") || strings.Contains(refusal.Detail, "Alice") {
		t.Fatalf("start without Bob's bot = %+v", refusal)
	}
}

// deleteLobby deletes a lobby's rows, children first, as lobby retention does.
func deleteLobby(t *testing.T, database *gorm.DB, lobbyID string) {
	t.Helper()
	for _, child := range []any{&lobbypostgres.LeaseRecord{}, &lobbypostgres.StartRosterRecord{}, &lobbypostgres.MembershipRecord{}, &lobbypostgres.TeamRecord{}} {
		if err := database.Where(clause.Eq{Column: clause.Column{Name: "lobby_id"}, Value: lobbyID}).Delete(child).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := database.Where(clause.Eq{Column: clause.Column{Name: "id"}, Value: lobbyID}).Delete(&lobbypostgres.RoomRecord{}).Error; err != nil {
		t.Fatal(err)
	}
}

func TestAQuizDemoLobbyGetsReadyWithoutABot(t *testing.T) {
	stack := newLobbyStack(t)
	alice, bob := stack.guest("Alice"), stack.guest("Bob")
	room := stack.room("quiz-demo", alice, bob)
	for _, member := range []guest{alice, bob} {
		stack.expect(stack.ready(room, member), http.StatusOK, nil)
	}
	var started domain.Snapshot
	stack.expect(stack.api.Post(stack.lobbyPath(room)+"/start", alice.header, map[string]any{}), http.StatusOK, &started)
	if started.Lobby.Phase != domain.PhaseStarted {
		t.Fatalf("quiz-demo phase = %s", started.Lobby.Phase)
	}
}
