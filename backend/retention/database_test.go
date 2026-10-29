package retention_test

import (
	"context"
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

// playedGame is a started two-team game whose first player sends soldiers
// every turn, so it has rows in every game table.
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
	binding, err := services.RunLobbyGame(context.Background(), database, game.service.Publisher, domain.Snapshot{
		Lobby:       domain.Lobby{ID: game.lobbyID, GameKey: "island-battles", Phase: domain.PhaseStarted},
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
	commands := []models.PublicGenerationCommand{}
	if player == 0 {
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
	_, err = game.service.Register(ctx, game.players[player], models.RegisterGenerationCommands{
		GameID: state.GameID, GenerationID: state.GenerationID, RequestID: uuid.NewString(), Complete: &complete, Commands: commands,
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
		"generation_requests":       unscoped.Model(&models.GenerationRequest{}).Where(inGame),
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
// templates and the migration histories.
func assertNoGameData(t *testing.T, database *gorm.DB) {
	t.Helper()
	var tables []string
	if err := database.Raw(`SELECT table_name FROM information_schema.tables WHERE table_schema = 'public' AND table_type = 'BASE TABLE'`).Scan(&tables).Error; err != nil {
		t.Fatal(err)
	}
	checked := 0
	for _, table := range tables {
		if strings.HasPrefix(table, "lobby_") || strings.HasSuffix(table, "_schema_migrations") || table == "map_templates" || table == "castle_templates" {
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
	if checked != 10 {
		t.Errorf("checked %d game tables, want the 10 that MigrateSchema creates; update countGameRows for a new one", checked)
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

// A turn holds its game's lock. A pass skips the game rather than waiting,
// and the turn's new generation then keeps it.
func TestAPassSkipsAGameWhoseTurnIsInProgress(t *testing.T) {
	database := testutils.InitTestDB(t)
	game := startGame(t, database)
	game.playTurns(t, 1)
	game.age(t, database, 8*24*time.Hour)
	before := countGameRows(t, database, game.gameID)

	locked, release, turnDone := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	go func() {
		turnDone <- dal.WithLockedGame(database, game.gameID, func(transaction *gorm.DB, _ *models.Game) error {
			close(locked)
			<-release
			state, err := dal.LoadGameState(transaction, game.gameID)
			if err != nil {
				return err
			}
			_, err = dal.SaveGeneration(transaction, game.gameID, state, nil, nil)
			return err
		})
	}()
	<-locked
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

// Turns and passes race on games that are just past the period. Either the
// turn commits first and keeps its game, or the pass retires the game and the
// turn finds it gone. A turn never adds rows to a retired game.
func TestPassesConcurrentWithTurnsAreSafe(t *testing.T) {
	database := testutils.InitTestDB(t)
	runner := newDatabaseRetention(t, database, 10)
	kept, removed := 0, 0
	for iteration := range 16 {
		game := startGame(t, database)
		game.playTurns(t, 1)
		// The first player has submitted; the second player's completion
		// creates the next generation, racing the pass.
		if err := game.submit(context.Background(), 0); err != nil {
			t.Fatal(err)
		}
		game.age(t, database, 8*24*time.Hour)
		generationsBefore := countGameRows(t, database, game.gameID)["generations"]

		var waitGroup sync.WaitGroup
		var turnErr, passErr error
		start := make(chan struct{})
		waitGroup.Add(2)
		go func() {
			defer waitGroup.Done()
			<-start
			turnErr = game.submit(context.Background(), 1)
		}()
		go func() {
			defer waitGroup.Done()
			<-start
			// Later passes start later, so both orders occur.
			time.Sleep(time.Duration(iteration) * 150 * time.Microsecond)
			_, passErr = runner.Pass(context.Background(), time.Now())
		}()
		close(start)
		waitGroup.Wait()
		if passErr != nil {
			t.Fatal(passErr)
		}
		drain(t, runner, 10)
		rows := countGameRows(t, database, game.gameID)
		switch {
		case turnErr == nil:
			// The turn's generation is new, so no pass may retire its game.
			if rows["games"] != 1 || rows["generations"] != generationsBefore+1 {
				t.Fatalf("rows after a committed turn = %v, want the game kept with %d generations", rows, generationsBefore+1)
			}
			kept++
		case errors.Is(turnErr, domain.ErrNotFound):
			if rows.total() != 0 {
				t.Fatalf("the turn found the game retired, but rows are left: %v", rows)
			}
			removed++
		default:
			t.Fatalf("turn error = %v", turnErr)
		}
	}
	t.Logf("kept %d games, removed %d", kept, removed)
}
