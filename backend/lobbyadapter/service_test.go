package lobbyadapter_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"islandBattles/backend/dal"
	"islandBattles/backend/internal/testutils"
	"islandBattles/backend/lobbyadapter"
	"islandBattles/backend/models"
	"islandBattles/backend/services"
	"islandBattles/lobby/domain"
	"islandBattles/lobby/gamehooks"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// hookSecret is the hooks' shared secret in tests.
var hookSecret = []byte(strings.Repeat("h", 40))

// hooksServer serves Island Battles' hooks over HTTP, as the backend does,
// and returns the URL of its hooks.
func hooksServer(t *testing.T, registration domain.GameRegistration) string {
	t.Helper()
	hooks, err := lobbyadapter.Hooks(hookSecret, registration)
	if err != nil {
		t.Fatal(err)
	}
	router := http.NewServeMux()
	router.Handle(lobbyadapter.HooksPath+"/", hooks)
	server := httptest.NewServer(router)
	t.Cleanup(server.Close)
	return server.URL + lobbyadapter.HooksPath
}

// hooksClient calls Island Battles' hooks through their signed handler, as
// the lobby does.
func hooksClient(t *testing.T, database *gorm.DB, publisher services.Publisher) *gamehooks.Client {
	t.Helper()
	client, err := gamehooks.NewClient(lobbyadapter.GameKey, hooksServer(t, lobbyadapter.Registration(database, publisher, nil, services.ServerBots{}, nil)), hookSecret)
	if err != nil {
		t.Fatal(err)
	}
	return client
}

// start calls the start hook and returns the game it bound.
func start(t *testing.T, client *gamehooks.Client, snapshot domain.Snapshot) (uint, error) {
	t.Helper()
	gameID, err := client.Start(context.Background(), snapshot)
	if err != nil {
		return 0, err
	}
	parsed, err := strconv.ParseUint(gameID, 10, 64)
	if err != nil {
		t.Fatalf("game_id %q: %v", gameID, err)
	}
	return uint(parsed), nil
}

func TestStartHookBindsAStableTeamMap(t *testing.T) {
	db := testutils.InitTestDB(t)
	snapshot := startedSnapshot()
	gameID, err := start(t, hooksClient(t, db, &testutils.RecordedPublisher{}), snapshot)
	if err != nil {
		t.Fatal(err)
	}
	var game models.Game
	if err := db.First(&game, gameID).Error; err != nil {
		t.Fatal(err)
	}
	if game.LobbyID == nil || *game.LobbyID != snapshot.Lobby.ID || game.LobbyRound != 1 {
		t.Fatalf("game lobby link = %v, round %d", game.LobbyID, game.LobbyRound)
	}
	records := teamBindings(t, db, gameID)
	if len(records) != 2 {
		t.Fatalf("persisted mappings = %#v", records)
	}
	for index, record := range records {
		if record.TeamID != snapshot.Teams[index].ID || record.TeamOrder != index+1 || record.TeamColor != uint(index+1) {
			t.Fatalf("binding %d = %#v", index, record)
		}
	}
}

func teamBindings(t *testing.T, db *gorm.DB, gameID uint) []models.TeamBindingRecord {
	t.Helper()
	var records []models.TeamBindingRecord
	if err := db.Where(&models.TeamBindingRecord{GameID: gameID}).Order("team_order").Find(&records).Error; err != nil {
		t.Fatal(err)
	}
	return records
}

func TestStartHookIsIdempotent(t *testing.T) {
	db := testutils.InitTestDB(t)
	snapshot := startedSnapshot()
	client := hooksClient(t, db, &testutils.RecordedPublisher{})
	first, err := start(t, client, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	second, err := start(t, client, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatalf("replay bound game %d, then %d", first, second)
	}
	assertBindingCounts(t, db, snapshot.Lobby.ID, 1, 2)
}

func TestStartHookConcurrentCalls(t *testing.T) {
	db := testutils.InitTestDB(t)
	snapshot := startedSnapshot()
	client := hooksClient(t, db, &testutils.RecordedPublisher{})
	begin := make(chan struct{})
	results := make(chan uint, 2)
	failures := make(chan error, 2)
	var group sync.WaitGroup
	for range 2 {
		group.Go(func() {
			<-begin
			gameID, err := start(t, client, snapshot)
			if err != nil {
				failures <- err
				return
			}
			results <- gameID
		})
	}
	close(begin)
	group.Wait()
	close(results)
	close(failures)
	for err := range failures {
		t.Fatal(err)
	}
	var first uint
	for gameID := range results {
		if first == 0 {
			first = gameID
		} else if first != gameID {
			t.Fatalf("concurrent game IDs = %d and %d", first, gameID)
		}
	}
	assertBindingCounts(t, db, snapshot.Lobby.ID, 1, 2)
}

// The lobby starts only a started lobby of Island Battles; the hook refuses
// anything else as unavailable, and binds nothing.
func TestStartHookRefusesALobbyThatIsNotAStartedIslandBattlesLobby(t *testing.T) {
	db := testutils.InitTestDB(t)
	client := hooksClient(t, db, &testutils.RecordedPublisher{})
	waiting := startedSnapshot()
	waiting.Lobby.Phase = domain.PhaseWaiting
	otherGame := startedSnapshot()
	otherGame.Lobby.GameKey = "mock-game"
	emptyRoster := startedSnapshot()
	emptyRoster.StartRoster = nil
	misnumbered := startedSnapshot()
	misnumbered.Teams[1].Order = 3
	for name, snapshot := range map[string]domain.Snapshot{"waiting": waiting, "other game": otherGame, "empty roster": emptyRoster, "misnumbered teams": misnumbered} {
		if _, err := start(t, client, snapshot); domain.ErrorCodeOf(err) != domain.CodeUnavailable {
			t.Errorf("%s: err = %v", name, err)
		}
		assertBindingCounts(t, db, snapshot.Lobby.ID, 0, 0)
	}
}

// Only the lobby, which knows the secret, calls the hooks: a request without
// a valid Lobby-Signature is 401 and runs nothing.
func TestHooksRefuseRequestsWithoutAValidSignature(t *testing.T) {
	db := testutils.InitTestDB(t)
	hooksURL := hooksServer(t, lobbyadapter.Registration(db, &testutils.RecordedPublisher{}, nil, services.ServerBots{}, nil))
	body, err := json.Marshal(gamehooks.StartRequest{Snapshot: startedSnapshot()})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	path := lobbyadapter.HooksPath + "/" + gamehooks.HookStart
	signatures := map[string]string{
		"none":         "",
		"other secret": gamehooks.Sign([]byte(strings.Repeat("x", 40)), now, http.MethodPost, path, body),
		"other path":   gamehooks.Sign(hookSecret, now, http.MethodPost, lobbyadapter.HooksPath+"/"+gamehooks.HookRoundEnded, body),
		"other body":   gamehooks.Sign(hookSecret, now, http.MethodPost, path, append(bytes.Clone(body), ' ')),
		"too old":      gamehooks.Sign(hookSecret, now.Add(-gamehooks.MaxClockSkew-time.Minute), http.MethodPost, path, body),
	}
	for _, hook := range gamehooks.Hooks {
		signatures["unsigned "+hook] = ""
	}
	for name, signature := range signatures {
		target := hooksURL + "/" + gamehooks.HookStart
		if hook, found := strings.CutPrefix(name, "unsigned "); found {
			target = hooksURL + "/" + hook
		}
		request, err := http.NewRequest(http.MethodPost, target, bytes.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		if signature != "" {
			request.Header.Set(gamehooks.SignatureHeader, signature)
		}
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		_ = response.Body.Close()
		if response.StatusCode != http.StatusUnauthorized {
			t.Errorf("%s: status %d, want 401", name, response.StatusCode)
		}
	}
	assertBindingCounts(t, db, startedSnapshot().Lobby.ID, 0, 0)
	var games int64
	if err := db.Model(&models.Game{}).Count(&games).Error; err != nil || games != 0 {
		t.Fatalf("%d games, error %v", games, err)
	}
}

func TestStartHookInitializesOnePlayableGenerationAndReplay(t *testing.T) {
	database := testutils.InitTestDB(t)
	snapshot := startedSnapshot()
	publisher := &testutils.RecordedPublisher{}
	client := hooksClient(t, database, publisher)
	gameID, err := start(t, client, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	game, err := dal.GetGameByID(database, gameID)
	if err != nil {
		t.Fatal(err)
	}
	if len(game.Generations) != 1 || len(game.Generations[0].Map.Castles) != 2 {
		t.Fatalf("initial state = %+v", game)
	}
	for _, castle := range game.Generations[0].Map.Castles {
		if castle.SoldierAmount != 30 {
			t.Fatal("wrong initial soldiers")
		}
	}
	// The game keeps the replay's teams, because its lobby is deleted later.
	teams := []models.GameTeam{{Color: 1, Name: "Blue", Players: []string{"Host"}}, {Color: 2, Name: "Red", Players: []string{"Guest"}}}
	if !reflect.DeepEqual(game.Teams, teams) || game.Outcome != nil {
		t.Fatalf("initial teams = %+v, outcome = %+v", game.Teams, game.Outcome)
	}
	// The frozen roster keeps each player's name from the start.
	var players []models.FrozenGamePlayer
	if err := database.Where(&models.FrozenGamePlayer{GameID: gameID}).Order("team_color").Find(&players).Error; err != nil {
		t.Fatal(err)
	}
	events := publisher.GenerationEvents(t)
	if len(players) != 2 || players[0].DisplayName != "Host" || players[1].DisplayName != "Guest" || len(events) != 2 || events[0].State.PlayerID == events[1].State.PlayerID || events[0].State.GenerationID != game.Generations[0].ID {
		t.Fatalf("players=%+v events=%+v", players, events)
	}
	if _, err := start(t, client, snapshot); err != nil {
		t.Fatal(err)
	}
	reloaded, err := dal.GetGameByID(database, gameID)
	if err != nil {
		t.Fatal(err)
	}
	if len(reloaded.Generations) != 1 || !reflect.DeepEqual(reloaded.Teams, game.Teams) || len(publisher.Publications()) != 2 {
		t.Fatal("retry changed initialized game or pushed it again")
	}
}

func startedSnapshot() domain.Snapshot {
	playerID, lobbyID := uuid.NewString(), uuid.NewString()
	host := playerID
	teamOne, teamTwo := uuid.NewString(), uuid.NewString()
	guestID := uuid.NewString()
	return domain.Snapshot{Lobby: domain.Lobby{ID: lobbyID, GameKey: lobbyadapter.GameKey, Phase: domain.PhaseStarted, Round: 1, HostPlayerID: &host}, Teams: []domain.Team{{ID: teamOne, LobbyID: lobbyID, Name: "Blue", Order: 1}, {ID: teamTwo, LobbyID: lobbyID, Name: "Red", Order: 2}}, StartRoster: domain.StartRoster{{PlayerID: playerID, TeamID: teamOne, MemberID: uuid.NewString(), TeamOrder: 1}, {PlayerID: guestID, TeamID: teamTwo, MemberID: uuid.NewString(), TeamOrder: 2}}, Members: []domain.Member{{PlayerID: playerID, DisplayName: "Host"}, {PlayerID: guestID, DisplayName: "Guest"}}}
}

func assertBindingCounts(t *testing.T, db *gorm.DB, lobbyID string, games, bindings int64) {
	t.Helper()
	var gameCount, bindingCount int64
	if err := db.Model(&models.Game{}).Where(&models.Game{LobbyID: &lobbyID}).Count(&gameCount).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&models.TeamBindingRecord{}).Count(&bindingCount).Error; err != nil {
		t.Fatal(err)
	}
	if gameCount != games || bindingCount != bindings {
		t.Fatalf("counts games=%d bindings=%d, want games=%d bindings=%d", gameCount, bindingCount, games, bindings)
	}
}

// A start that fails answers unavailable, and the lobby's next start, which
// calls the hook again, binds the game.
func TestStartHookMappingFailureRollsBackGame(t *testing.T) {
	db := testutils.InitTestDB(t)
	snapshot := startedSnapshot()
	failure := errors.New("injected mapping failure")
	callback := "test:fail-team-bindings"
	if err := db.Callback().Create().Before("gorm:create").Register(callback, func(transaction *gorm.DB) {
		if transaction.Statement.Table == "island_game_team_bindings" {
			_ = transaction.AddError(failure)
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Callback().Create().Remove(callback) })
	if gameID, err := start(t, hooksClient(t, db, &testutils.RecordedPublisher{}), snapshot); domain.ErrorCodeOf(err) != domain.CodeUnavailable || gameID != 0 {
		t.Fatalf("game=%d error=%v", gameID, err)
	}
	assertBindingCounts(t, db, snapshot.Lobby.ID, 0, 0)
}

func TestStartHookRefusesAnIncompletePersistedMapping(t *testing.T) {
	db := testutils.InitTestDB(t)
	snapshot := startedSnapshot()
	client := hooksClient(t, db, &testutils.RecordedPublisher{})
	gameID, err := start(t, client, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Where(&models.TeamBindingRecord{GameID: gameID, TeamID: snapshot.Teams[0].ID}).Delete(&models.TeamBindingRecord{}).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := start(t, client, snapshot); domain.ErrorCodeOf(err) != domain.CodeUnavailable {
		t.Fatalf("incomplete mapping error=%v", err)
	}
	assertBindingCounts(t, db, snapshot.Lobby.ID, 1, 1)
}

func TestFailedInitializationRollsBackAndCanBeRetried(t *testing.T) {
	database := testutils.InitTestDB(t)
	snapshot := startedSnapshot()
	publisher := &testutils.RecordedPublisher{}
	client := hooksClient(t, database, publisher)
	failure := errors.New("injected generation failure")
	created := 0
	callback := "test:fail-simulation"
	if err := database.Callback().Create().Before("gorm:create").Register(callback, func(transaction *gorm.DB) {
		if transaction.Statement.Table == "generations" {
			created++
			if created == 1 {
				_ = transaction.AddError(failure)
			}
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Callback().Create().Remove(callback) })
	if _, err := start(t, client, snapshot); domain.ErrorCodeOf(err) != domain.CodeUnavailable {
		t.Fatalf("unexpected failure: %v", err)
	}
	var generations int64
	if err := database.Model(&models.Generation{}).Count(&generations).Error; err != nil {
		t.Fatal(err)
	}
	if generations != 0 {
		t.Fatalf("left %d partial generations", generations)
	}
	if len(publisher.Publications()) != 0 {
		t.Fatal("pushed a generation that rolled back")
	}
	for _, model := range []any{&models.Map{}, &models.Castle{}, &models.FrozenGamePlayer{}} {
		var count int64
		if err := database.Model(model).Count(&count).Error; err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Fatalf("left %d partial %T records", count, model)
		}
	}
	if game, err := dal.FindLobbyGame(database, snapshot.Lobby.ID); err != nil || len(game.Teams) != 0 {
		t.Fatalf("partial teams: %+v; error: %v", game, err)
	}
	if _, err := start(t, client, snapshot); err != nil {
		t.Fatal(err)
	}
	if game, err := dal.FindLobbyGame(database, snapshot.Lobby.ID); err != nil || len(game.Teams) == 0 {
		t.Fatalf("retry did not save teams: %v", err)
	}
	assertBindingCounts(t, database, snapshot.Lobby.ID, 1, 2)
}

// The lobby's configuration of Island Battles, LobbyGame, starts a lobby only
// once every team has a player.
func TestLobbyGameRequiresEveryTeamFilled(t *testing.T) {
	games := configuredGames(t)
	if len(games) != 1 || games[0].Registration.Key != lobbyadapter.GameKey || !games[0].Registration.StartRules.EveryTeamNeedsMember {
		t.Fatalf("configured games = %+v", games)
	}
}

// configuredGames reads LobbyGame as the lobby reads LOBBY_GAMES.
func configuredGames(t *testing.T) []gamehooks.ConfiguredGame {
	t.Helper()
	entry, err := lobbyadapter.LobbyGame("https://island-battles.example.test"+lobbyadapter.HooksPath, "")
	if err != nil {
		t.Fatal(err)
	}
	games, err := gamehooks.ParseGames("["+string(entry)+"]", func(name string) (string, bool) {
		return string(hookSecret), name == lobbyadapter.SecretEnv
	})
	if err != nil {
		t.Fatal(err)
	}
	return games
}
