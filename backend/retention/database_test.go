package retention_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"islandBattles/backend/dal"
	"islandBattles/backend/internal/testutils"
	"islandBattles/backend/models"
	"islandBattles/backend/retention"
	"islandBattles/backend/services"
	"islandBattles/lobby/domain"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// playedGame is a started two-team game whose first player sends soldiers,
// logs a line and keeps bot memory every turn, so it has rows in every game
// table.
type playedGame struct {
	gameID  uint
	lobbyID string
	players []domain.Principal
	service *services.GenerationService
}

func startGame(t *testing.T, database *gorm.DB) playedGame {
	t.Helper()
	game := playedGame{
		lobbyID: uuid.NewString(),
		players: []domain.Principal{{PlayerID: uuid.NewString()}, {PlayerID: uuid.NewString()}},
		service: &services.GenerationService{Database: database, Publisher: &testutils.RecordedPublisher{}},
	}
	blue, red := uuid.NewString(), uuid.NewString()
	binding, err := services.RunLobbyGame(context.Background(), database, game.service.Publisher, nil, domain.Snapshot{
		// Retention ages games by days, which no turn deadline would survive.
		Lobby:       domain.Lobby{ID: game.lobbyID, GameKey: "island-battles", Phase: domain.PhaseStarted, GameSettings: json.RawMessage(`{"turn_timeout_seconds":null}`)},
		Teams:       []domain.Team{{ID: blue, Order: 1, Name: "Blue"}, {ID: red, Order: 2, Name: "Red"}},
		StartRoster: domain.StartRoster{{PlayerID: game.players[0].PlayerID, TeamID: blue}, {PlayerID: game.players[1].PlayerID, TeamID: red}},
	})
	if err != nil {
		t.Fatal(err)
	}
	game.gameID = binding.GameID
	return game
}

// playTurns plays whole turns: the first player sends one soldier from its
// castle to the enemy's, and both complete.
func (game playedGame) playTurns(t *testing.T, turns int) {
	t.Helper()
	for range turns {
		if err := game.playTurn(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
}

func (game playedGame) playTurn(ctx context.Context) error {
	if err := game.submit(ctx, 0); err != nil {
		return err
	}
	return game.submit(ctx, 1)
}

// submit completes one player's turn, the first player with one command.
func (game playedGame) submit(ctx context.Context, player int) error {
	state, err := game.service.Current(ctx, game.players[player], game.lobbyID)
	if err != nil {
		return err
	}
	return game.register(ctx, player, state)
}

// register completes one player's turn on state's generation, the first
// player with one command, one game log line and its bot memory.
func (game playedGame) register(ctx context.Context, player int, state models.GenerationState) error {
	commands := []models.PublicGenerationCommand{}
	var logs []models.GenerationLogLine
	var memory models.BotMemory
	if player == 0 {
		logs = []models.GenerationLogLine{{Level: models.GenerationLogInfo, Logger: "island_battles.player", Message: "attacking"}}
		memory = fmt.Appendf(nil, `{"generation_id":%d}`, state.GenerationID)
		var origin, destination models.CastleView
		for _, castle := range state.Generation.Castles {
			if castle.TeamColor == state.TeamColor {
				origin = castle
			} else if castle.TeamColor != models.NeutralTeamColor {
				destination = castle
			}
		}
		if origin.Soldiers > 1 {
			commands = append(commands, models.PublicGenerationCommand{CommandType: models.SendSoldierGroup,
				Parameters: fmt.Appendf(nil, `{"origin_castle_id":%d,"destination_castle_id":%d,"soldier_amount":1}`, origin.ID, destination.ID)})
		}
	}
	complete := true
	_, err := game.service.Register(ctx, game.players[player], models.RegisterGenerationCommands{
		GameID: state.GameID, GenerationID: state.GenerationID, RequestID: uuid.NewString(), Complete: &complete, Commands: commands, Logs: logs, Memory: memory,
	})
	return err
}

// age moves the game and all its generations back by age, as if it had last
// been played then.
func (game playedGame) age(t *testing.T, database *gorm.DB, age time.Duration) {
	t.Helper()
	moved := clause.Expr{SQL: "created_at - make_interval(secs => ?)", Vars: []any{age.Seconds()}}
	if err := database.Model(&models.Game{}).Where(&models.Game{Model: gorm.Model{ID: game.gameID}}).UpdateColumn("created_at", moved).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.Model(&models.Generation{}).Where(&models.Generation{GameID: game.gameID}).UpdateColumn("created_at", moved).Error; err != nil {
		t.Fatal(err)
	}
}

// gameRows counts a game's rows in every game table, soft-deleted or not.
type gameRows map[string]int64

func countGameRows(t *testing.T, database *gorm.DB, gameID uint) gameRows {
	t.Helper()
	unscoped := database.Unscoped().Session(&gorm.Session{})
	var generations []models.Generation
	if err := unscoped.Select("ID", "MapID").Where(&models.Generation{GameID: gameID}).Find(&generations).Error; err != nil {
		t.Fatal(err)
	}
	generationIDs, mapIDs := []any{0}, []any{0}
	for _, generation := range generations {
		generationIDs = append(generationIDs, generation.ID)
		mapIDs = append(mapIDs, generation.MapID)
	}
	inGenerations := clause.IN{Column: clause.Column{Name: "generation_id"}, Values: generationIDs}
	inGame := clause.Eq{Column: clause.Column{Name: "game_id"}, Value: gameID}
	rows := gameRows{}
	for table, query := range map[string]*gorm.DB{
		"games":                     unscoped.Model(&models.Game{}).Where(clause.Eq{Column: clause.Column{Name: "id"}, Value: gameID}),
		"generations":               unscoped.Model(&models.Generation{}).Where(inGame),
		"maps":                      unscoped.Model(&models.Map{}).Where(clause.IN{Column: clause.Column{Name: "id"}, Values: mapIDs}),
		"castles":                   unscoped.Model(&models.Castle{}).Where(clause.IN{Column: clause.Column{Name: "map_id"}, Values: mapIDs}),
		"soldier_groups":            unscoped.Model(&models.SoldierGroup{}).Where(inGenerations),
		"commands":                  unscoped.Model(&models.Command{}).Where(inGenerations),
		"generation_inputs":         unscoped.Model(&models.GenerationInput{}).Where(inGenerations),
		"generation_logs":           unscoped.Model(&models.GenerationLog{}).Where(inGenerations),
		"generation_requests":       unscoped.Model(&models.GenerationRequest{}).Where(inGame),
		"game_player_bot_memory":    unscoped.Model(&models.GamePlayerBotMemory{}).Where(inGame),
		"frozen_game_players":       unscoped.Model(&models.FrozenGamePlayer{}).Where(inGame),
		"island_game_team_bindings": unscoped.Model(&models.TeamBindingRecord{}).Where(inGame),
	} {
		var count int64
		if err := query.Count(&count).Error; err != nil {
			t.Fatal(err)
		}
		rows[table] = count
	}
	return rows
}

func (rows gameRows) total() int64 {
	var total int64
	for _, count := range rows {
		total += count
	}
	return total
}

// assertNoGameData checks that every game table is empty, including any that
// countGameRows doesn't know about: every table but the lobby's, the map
// templates, the migration histories and the bot runners' cached compute.
func assertNoGameData(t *testing.T, database *gorm.DB) {
	t.Helper()
	var tables []string
	if err := database.Raw(`SELECT table_name FROM information_schema.tables WHERE table_schema = 'public' AND table_type = 'BASE TABLE'`).Scan(&tables).Error; err != nil {
		t.Fatal(err)
	}
	checked := 0
	for _, table := range tables {
		// island_bot_compute_usage caches the bot runners' compute, which
		// belongs to no game.
		if strings.HasPrefix(table, "lobby_") || strings.HasSuffix(table, "_schema_migrations") || table == "map_templates" || table == "castle_templates" || table == "island_bot_compute_usage" {
			continue
		}
		checked++
		var count int64
		if err := database.Table(table).Count(&count).Error; err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Errorf("game table %s still has %d rows", table, count)
		}
	}
	// The 12 tables of games, and island_bot_packages, which belongs to
	// lobbies rather than games and is deleted on its own schedule.
	if checked != 13 {
		t.Errorf("checked %d game tables, want the 13 that schema.Migrate creates; update countGameRows for a new one", checked)
	}
}

func countMapTemplates(t *testing.T, database *gorm.DB) (templates, castles int64) {
	t.Helper()
	if err := database.Unscoped().Model(&models.MapTemplate{}).Count(&templates).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.Unscoped().Model(&models.CastleTemplate{}).Count(&castles).Error; err != nil {
		t.Fatal(err)
	}
	return templates, castles
}

func newDatabaseRetention(t *testing.T, database *gorm.DB, batchSize int) *retention.Retention {
	t.Helper()
	policy := retention.DefaultPolicy()
	policy.BatchSize = batchSize
	runner, err := retention.New(retention.DatabaseStore{Database: database}, policy, time.Now, discardLogger())
	if err != nil {
		t.Fatal(err)
	}
	return runner
}

// drain runs passes until one finds nothing to do, and returns how many did
// something.
func drain(t *testing.T, runner *retention.Retention, batchSize int) int {
	t.Helper()
	for passes := 0; ; passes++ {
		if passes == 1000 {
			t.Fatal("retention never finished")
		}
		result, err := runner.Pass(context.Background(), time.Now())
		if err != nil {
			t.Fatal(err)
		}
		if result.RetiredGames > batchSize || result.Generations > batchSize || result.Games > batchSize {
			t.Fatalf("a pass exceeded its batch of %d: %+v", batchSize, result)
		}
		if result == (retention.Result{}) {
			return passes
		}
	}
}

func TestAnOldGameIsRemovedWithAllItsRowsAndMapTemplatesAreKept(t *testing.T) {
	database := testutils.InitTestDB(t)
	// A default map, and an old game started on it, whose map is a copy.
	onTemplate, err := dal.GetGeneration(database, testutils.CreateGenerationForTest(t, database))
	if err != nil {
		t.Fatal(err)
	}
	playedGame{gameID: onTemplate.GameID}.age(t, database, 30*24*time.Hour)
	templates, templateCastles := countMapTemplates(t, database)
	if templates == 0 || templateCastles == 0 {
		t.Fatal("the fixture stored no map template")
	}
	game := startGame(t, database)
	game.playTurns(t, 3)
	before := countGameRows(t, database, game.gameID)
	for table, count := range before {
		if count == 0 {
			t.Fatalf("the fixture game has no %s rows: %v", table, before)
		}
	}
	game.age(t, database, 7*24*time.Hour+time.Minute)
	// Ended games and abandoned ones are treated alike.
	if err := database.Model(&models.Game{Model: gorm.Model{ID: game.gameID}}).Select("Outcome").
		Updates(&models.Game{Outcome: &models.GameOutcome{Reason: models.GameOutcomeGenerationCap}}).Error; err != nil {
		t.Fatal(err)
	}

	runner := newDatabaseRetention(t, database, 2)
	result, err := runner.Pass(context.Background(), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if result.RetiredGames != 2 {
		t.Fatalf("first pass = %+v, want both old games retired", result)
	}
	// A retired game is gone for every request before its rows are.
	if _, err := game.service.Current(context.Background(), game.players[0], game.lobbyID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("Current() on a retired game error = %v, want not found", err)
	}
	if err := game.playTurn(context.Background()); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("a turn on a retired game error = %v, want not found", err)
	}
	drain(t, runner, 2)
	if rows := countGameRows(t, database, game.gameID); rows.total() != 0 {
		t.Fatalf("rows left after retention: %v", rows)
	}
	assertNoGameData(t, database)
	if afterTemplates, afterCastles := countMapTemplates(t, database); afterTemplates != templates || afterCastles != templateCastles {
		t.Fatalf("map templates=%d castle templates=%d, want %d and %d kept", afterTemplates, afterCastles, templates, templateCastles)
	}
}

func TestAGameWithANewerGenerationIsKept(t *testing.T) {
	database := testutils.InitTestDB(t)
	recent := startGame(t, database)
	recent.playTurns(t, 2)
	// The game started long ago, but its latest turn was just now.
	if err := database.Model(&models.Game{}).Where(&models.Game{Model: gorm.Model{ID: recent.gameID}}).
		UpdateColumn("created_at", time.Now().Add(-30*24*time.Hour)).Error; err != nil {
		t.Fatal(err)
	}
	var first []models.Generation
	if err := database.Where(&models.Generation{GameID: recent.gameID}).Order(clause.OrderByColumn{Column: clause.Column{Name: "id"}}).Limit(1).Find(&first).Error; err != nil || len(first) != 1 {
		t.Fatalf("first generation: %v", err)
	}
	if err := database.Model(&first[0]).UpdateColumn("created_at", time.Now().Add(-30*24*time.Hour)).Error; err != nil {
		t.Fatal(err)
	}
	// A game that never got a generation counts from its creation.
	oldEmpty, err := dal.CreateGame(database)
	if err != nil {
		t.Fatal(err)
	}
	playedGame{gameID: oldEmpty}.age(t, database, 8*24*time.Hour)
	newEmpty, err := dal.CreateGame(database)
	if err != nil {
		t.Fatal(err)
	}
	// A game played up to just inside the period is kept too.
	almost := startGame(t, database)
	almost.playTurns(t, 1)
	almost.age(t, database, 7*24*time.Hour-time.Hour)
	old := startGame(t, database)
	old.playTurns(t, 1)
	old.age(t, database, 8*24*time.Hour)
	recentRows, almostRows := countGameRows(t, database, recent.gameID), countGameRows(t, database, almost.gameID)

	drain(t, newDatabaseRetention(t, database, 10), 10)
	if rows := countGameRows(t, database, old.gameID); rows.total() != 0 {
		t.Fatalf("the old game was not removed: %v", rows)
	}
	if rows := countGameRows(t, database, oldEmpty); rows.total() != 0 {
		t.Fatalf("the old game without generations was not removed: %v", rows)
	}
	if rows := countGameRows(t, database, newEmpty); rows["games"] != 1 {
		t.Fatalf("the new game without generations was removed: %v", rows)
	}
	for name, kept := range map[string]struct {
		game playedGame
		rows gameRows
	}{"recent": {recent, recentRows}, "almost expired": {almost, almostRows}} {
		if rows := countGameRows(t, database, kept.game.gameID); len(rows) != len(kept.rows) || rows.total() != kept.rows.total() {
			t.Fatalf("%s game rows = %v, want %v kept", name, rows, kept.rows)
		}
		if err := kept.game.playTurn(context.Background()); err != nil {
			t.Fatalf("%s game cannot be played after retention: %v", name, err)
		}
	}
}

func TestALargeBacklogIsRemovedOverSeveralPasses(t *testing.T) {
	database := testutils.InitTestDB(t)
	const games, turns, batchSize = 6, 4, 3
	for range games {
		game := startGame(t, database)
		game.playTurns(t, turns)
		game.age(t, database, 30*24*time.Hour)
	}
	kept := startGame(t, database)
	kept.playTurns(t, 1)
	keptRows := countGameRows(t, database, kept.gameID)

	// Each game has its first generation and one per turn.
	runner := newDatabaseRetention(t, database, batchSize)
	passes := drain(t, runner, batchSize)
	if minimum := games * (turns + 1) / batchSize; passes < minimum {
		t.Fatalf("the backlog took %d passes, want at least %d batches of %d generations", passes, minimum, batchSize)
	}
	if rows := countGameRows(t, database, kept.gameID); rows.total() != keptRows.total() {
		t.Fatalf("the playing game lost rows: %v, want %v", rows, keptRows)
	}
	var remainingGames int64
	if err := database.Unscoped().Model(&models.Game{}).Count(&remainingGames).Error; err != nil || remainingGames != 1 {
		t.Fatalf("games left = %d (%v), want only the playing one", remainingGames, err)
	}
}

// Requests drain a backlog one batch each, and stop running passes once it's
// gone.
func TestRequestsDrainABacklogAndThenWaitForTheInterval(t *testing.T) {
	database := testutils.InitTestDB(t)
	for range 3 {
		game := startGame(t, database)
		game.playTurns(t, 2)
		game.age(t, database, 30*24*time.Hour)
	}
	clock := &fakeClock{now: time.Now()}
	policy := retention.DefaultPolicy()
	policy.BatchSize = 2
	runner, err := retention.New(retention.DatabaseStore{Database: database}, policy, clock.Now, discardLogger())
	if err != nil {
		t.Fatal(err)
	}
	requests := 0
	for ; countAllGames(t, database) > 0; requests++ {
		if requests == 100 {
			t.Fatal("requests never drained the backlog")
		}
		runner.RunIfDue(context.Background())
	}
	if requests < 5 {
		t.Fatalf("the backlog of 9 generations took %d requests, want at least 5 batches of 2", requests)
	}
	// One more pass finds the backlog gone, and then passes wait.
	runner.RunIfDue(context.Background())
	fresh := startGame(t, database)
	fresh.age(t, database, 30*24*time.Hour)
	runner.RunIfDue(context.Background())
	if countAllGames(t, database) != 1 {
		t.Fatal("a request ran a pass within the interval after the backlog was gone")
	}
	clock.now = clock.now.Add(policy.PassInterval)
	runner.RunIfDue(context.Background())
	runner.RunIfDue(context.Background())
	if countAllGames(t, database) != 0 {
		t.Fatal("requests after the interval did not remove the expired game")
	}
}

func countAllGames(t *testing.T, database *gorm.DB) int64 {
	t.Helper()
	var count int64
	if err := database.Unscoped().Model(&models.Game{}).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	return count
}

// holdOnce registers a callback that, the first time a statement matches,
// signals held and waits for release, inside that statement's transaction.
func holdOnce(t *testing.T, processor interface {
	Register(string, func(*gorm.DB)) error
}, remove func(string) error, name string, matches func(*gorm.DB) bool) (held, release chan struct{}) {
	t.Helper()
	held, release = make(chan struct{}), make(chan struct{})
	var once sync.Once
	if err := processor.Register(name, func(statement *gorm.DB) {
		if matches(statement) {
			once.Do(func() {
				close(held)
				<-release
			})
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = remove(name) })
	return held, release
}

// A turn that is saving its new generation holds its game's lock. A pass
// skips the game rather than waiting, and the turn's new generation then
// keeps it.
func TestAPassSkipsAGameWhoseTurnIsInProgress(t *testing.T) {
	database := testutils.InitTestDB(t)
	game := startGame(t, database)
	game.playTurns(t, 1)
	if err := game.submit(context.Background(), 0); err != nil {
		t.Fatal(err)
	}
	game.age(t, database, 8*24*time.Hour)
	before := countGameRows(t, database, game.gameID)
	state, err := game.service.Current(context.Background(), game.players[1], game.lobbyID)
	if err != nil {
		t.Fatal(err)
	}

	// The second player's completion stops right after inserting the new
	// generation, inside the turn's transaction.
	inTurn, release := holdOnce(t, database.Callback().Create().After("gorm:create"), database.Callback().Create().Remove,
		"test:hold_turn", func(statement *gorm.DB) bool { return statement.Statement.Table == "generations" })
	turnDone := make(chan error, 1)
	go func() { turnDone <- game.register(context.Background(), 1, state) }()
	<-inTurn
	runner := newDatabaseRetention(t, database, 10)
	started := time.Now()
	result, err := runner.Pass(context.Background(), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if waited := time.Since(started); waited > 2*time.Second {
		t.Fatalf("the pass waited %v for the turn", waited)
	}
	if result != (retention.Result{}) {
		t.Fatalf("pass during the turn = %+v, want the game skipped", result)
	}
	close(release)
	if err := <-turnDone; err != nil {
		t.Fatal(err)
	}
	drain(t, runner, 10)
	if rows := countGameRows(t, database, game.gameID); rows["generations"] != before["generations"]+1 || rows["games"] != 1 {
		t.Fatalf("rows after the turn = %v, want the game kept with its new generation", rows)
	}
	if err := game.playTurn(context.Background()); err != nil {
		t.Fatalf("the game cannot be played after retention: %v", err)
	}
}

// A turn that arrives while a pass is retiring its game waits for the game's
// lock, then finds the game gone, and stores nothing.
func TestATurnThatArrivesWhileAPassRetiresItsGameFindsItGone(t *testing.T) {
	database := testutils.InitTestDB(t)
	// The pass and the waiting turn each hold a connection, and the test
	// needs one more to see the turn wait.
	pool, err := database.DB()
	if err != nil {
		t.Fatal(err)
	}
	pool.SetMaxOpenConns(4)
	game := startGame(t, database)
	game.playTurns(t, 1)
	if err := game.submit(context.Background(), 0); err != nil {
		t.Fatal(err)
	}
	game.age(t, database, 8*24*time.Hour)
	state, err := game.service.Current(context.Background(), game.players[1], game.lobbyID)
	if err != nil {
		t.Fatal(err)
	}

	// The pass stops right after soft-deleting the game, inside its
	// transaction, so it still holds the game's lock.
	retiring, release := holdOnce(t, database.Callback().Delete().After("gorm:delete"), database.Callback().Delete().Remove,
		"test:hold_retirement", func(statement *gorm.DB) bool {
			return statement.Statement.Table == "games" && !statement.Statement.Unscoped
		})
	runner := newDatabaseRetention(t, database, 10)
	passDone := make(chan error, 1)
	go func() {
		_, err := runner.Pass(context.Background(), time.Now())
		passDone <- err
	}()
	<-retiring
	turnDone := make(chan error, 1)
	go func() { turnDone <- game.register(context.Background(), 1, state) }()
	waitForALockWait(t, database)
	close(release)
	if err := <-passDone; err != nil {
		t.Fatal(err)
	}
	if err := <-turnDone; !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("turn error = %v, want not found", err)
	}
	drain(t, runner, 10)
	if rows := countGameRows(t, database, game.gameID); rows.total() != 0 {
		t.Fatalf("rows left after the turn found the game gone: %v", rows)
	}
}

// waitForALockWait waits until a query is blocked on a row lock.
func waitForALockWait(t *testing.T, database *gorm.DB) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		var waiting int64
		if err := database.Raw(`SELECT count(*) FROM pg_stat_activity WHERE wait_event_type = 'Lock'`).Scan(&waiting).Error; err != nil {
			t.Fatal(err)
		}
		if waiting > 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("the turn never waited for the game's lock")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// A game whose purge always fails is rolled back and retried by later
// passes, while the other retired games are still deleted.
func TestAGameThatFailsToPurgeDoesNotBlockTheOthers(t *testing.T) {
	database := testutils.InitTestDB(t)
	broken := startGame(t, database)
	broken.playTurns(t, 1)
	broken.age(t, database, 8*24*time.Hour)
	healthy := startGame(t, database)
	healthy.playTurns(t, 1)
	healthy.age(t, database, 8*24*time.Hour)
	brokenRows := countGameRows(t, database, broken.gameID)

	// A row outside the game tables references one of the broken game's
	// castles, so deleting that game's castles always fails.
	var castleIDs []uint
	if err := database.Model(&models.Castle{}).Joins("JOIN generations ON generations.map_id = castles.map_id").
		Where(clause.Eq{Column: clause.Column{Table: "generations", Name: "game_id"}, Value: broken.gameID}).
		Order(clause.OrderByColumn{Column: clause.Column{Table: "castles", Name: "id"}}).Limit(1).
		Pluck("castles.id", &castleIDs).Error; err != nil || len(castleIDs) != 1 {
		t.Fatalf("castle of the broken game: %v %v", castleIDs, err)
	}
	if err := database.Exec(`CREATE TABLE retention_test_blockers (castle_id bigint REFERENCES castles(id))`).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.Exec(`INSERT INTO retention_test_blockers VALUES (?)`, castleIDs[0]).Error; err != nil {
		t.Fatal(err)
	}

	runner := newDatabaseRetention(t, database, 10)
	for pass := range 2 {
		result, err := runner.Pass(context.Background(), time.Now())
		if err == nil || !strings.Contains(err.Error(), fmt.Sprintf("purge retired game %d", broken.gameID)) {
			t.Fatalf("pass %d error = %v, want the broken game's purge to fail", pass, err)
		}
		if pass == 0 && (result.RetiredGames != 2 || result.Games != 1) {
			t.Fatalf("first pass = %+v, want both games retired and the healthy one deleted", result)
		}
		if pass == 1 && result.Games != 0 {
			t.Fatalf("second pass = %+v, want nothing more deleted", result)
		}
	}
	if rows := countGameRows(t, database, healthy.gameID); rows.total() != 0 {
		t.Fatalf("the healthy game was not deleted: %v", rows)
	}
	// The broken game's purge rolled back whole, and the game stays hidden.
	if rows := countGameRows(t, database, broken.gameID); rows.total() != brokenRows.total() {
		t.Fatalf("broken game rows = %v, want all of %v kept", rows, brokenRows)
	}
	if _, err := dal.FindGame(database, broken.gameID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("FindGame() on the broken game error = %v, want not found", err)
	}

	// Once the obstacle is gone, a later pass deletes it.
	if err := database.Exec(`DROP TABLE retention_test_blockers`).Error; err != nil {
		t.Fatal(err)
	}
	drain(t, runner, 10)
	if rows := countGameRows(t, database, broken.gameID); rows.total() != 0 {
		t.Fatalf("the broken game was not deleted once it could be: %v", rows)
	}
	assertNoGameData(t, database)
}

// countStatements counts every statement GORM sends from now on. On Lambda
// each is a round trip to the database, as are the BEGIN and COMMIT of each
// transaction.
func countStatements(t *testing.T, database *gorm.DB) *int {
	t.Helper()
	count := new(int)
	var mutex sync.Mutex
	callbacks := database.Callback()
	for _, processor := range []interface {
		Register(string, func(*gorm.DB)) error
		Remove(string) error
	}{callbacks.Create(), callbacks.Query(), callbacks.Update(), callbacks.Delete(), callbacks.Row(), callbacks.Raw()} {
		if err := processor.Register("test:count_statements", func(statement *gorm.DB) {
			// Subqueries are built in dry runs, which send nothing.
			if statement.DryRun {
				return
			}
			mutex.Lock()
			defer mutex.Unlock()
			*count++
		}); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = processor.Remove("test:count_statements") })
	}
	return count
}

func TestAPassMakesFewDatabaseRoundTrips(t *testing.T) {
	database := testutils.InitTestDB(t)
	for range 5 {
		game := startGame(t, database)
		game.playTurns(t, 1)
		game.age(t, database, 30*24*time.Hour)
	}
	long := startGame(t, database)
	long.playTurns(t, 14)
	long.age(t, database, 30*24*time.Hour)
	runner := newDatabaseRetention(t, database, 10)
	statements := countStatements(t, database)

	// Retiring all 6 games, and deleting the first 3 small games whole: one
	// transaction to retire, and one per game. Deleting old bot packages is
	// one statement more.
	result, err := runner.Pass(context.Background(), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if result.RetiredGames != 6 || result.Games != retention.MaxPurgedGames || result.Generations != 2*retention.MaxPurgedGames {
		t.Fatalf("first pass = %+v", result)
	}
	if *statements > 49 {
		t.Fatalf("the pass sent %d statements, want at most 49", *statements)
	}
	t.Logf("a pass that deletes 3 small games sends %d statements in 4 transactions", *statements)
	drain(t, runner, 10)

	*statements = 0
	if result, err := runner.Pass(context.Background(), time.Now()); err != nil || result != (retention.Result{}) {
		t.Fatalf("idle pass = %+v, %v", result, err)
	}
	// One to find games to retire, one to find retired games, and one to
	// delete old bot packages.
	if *statements != 3 {
		t.Fatalf("a pass with nothing to delete sent %d statements, want 3", *statements)
	}
}

func TestOldBotPackageRowsAreDeleted(t *testing.T) {
	database := testutils.InitTestDB(t)
	now := time.Now()
	lobbyID := uuid.NewString()
	ages := map[string]time.Duration{"fresh": time.Hour, "a week": 7 * 24 * time.Hour, "just over 8 days": 8*24*time.Hour + time.Minute, "a month": 30 * 24 * time.Hour}
	ids := map[string]string{}
	for name, age := range ages {
		ids[name] = uuid.NewString()
		uploaded := now.Add(-age)
		if err := dal.CreateBotPackage(database, models.BotPackage{PackageID: ids[name], LobbyID: lobbyID, OwnerPlayerID: uuid.NewString(), OwnerName: name,
			Status: models.BotPackageReady, S3Key: "lobbies/" + lobbyID + "/" + ids[name] + ".zip", Language: "native", EntryOrCommand: "./bot worker", SizeBytes: 1, UploadedAt: &uploaded, CreatedAt: uploaded}); err != nil {
			t.Fatal(err)
		}
	}
	result, err := newDatabaseRetention(t, database, 10).Pass(context.Background(), now)
	if err != nil || result.BotPackages != 2 {
		t.Fatalf("result = %+v, %v; want the 2 packages older than 8 days deleted", result, err)
	}
	for name := range ages {
		found, err := dal.FindBotPackage(database, lobbyID, ids[name])
		if err != nil {
			t.Fatal(err)
		}
		if kept := found != nil; kept != (ages[name] < retention.BotPackagePeriod) {
			t.Errorf("%s: kept = %v", name, kept)
		}
	}
	// A batch is bounded, oldest first.
	for range 3 {
		old := now.Add(-20 * 24 * time.Hour)
		if err := dal.CreateBotPackage(database, models.BotPackage{PackageID: uuid.NewString(), LobbyID: lobbyID, OwnerPlayerID: uuid.NewString(),
			Status: models.BotPackagePending, S3Key: "k", CreatedAt: old}); err != nil {
			t.Fatal(err)
		}
	}
	if deleted, err := dal.DeleteExpiredBotPackages(database, now.Add(-retention.BotPackagePeriod), 2); err != nil || deleted != 2 {
		t.Fatalf("deleted = %d, %v; want a batch of 2", deleted, err)
	}
}
