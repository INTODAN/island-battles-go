package services_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"islandBattles/backend/dal"
	"islandBattles/backend/internal/testutils"
	"islandBattles/backend/models"
	"islandBattles/backend/services"
	"islandBattles/lobby/domain"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// goBotGame is a started game of two teams, Blue and Red, with one player
// each, whose service runs on a clock the test sets.
type goBotGame struct {
	database  *gorm.DB
	publisher *testutils.RecordedPublisher
	service   *services.GenerationService
	lobbyID   string
	gameID    uint
	clock     *time.Time
	blue, red domain.Principal
}

// startGoBotGame starts a game with the lobby settings, where the backend
// plays Blue and Red with the default bots of blueLevel and redLevel, and 0
// leaves the player to their client.
func startGoBotGame(t *testing.T, settings string, blueLevel, redLevel uint) goBotGame {
	t.Helper()
	game, _ := startGoBotTeams(t, settings, []uint{blueLevel}, []uint{redLevel})
	return game
}

// goBotPlayerNames are the display names of each team's players.
var goBotPlayerNames = [][]string{{"Alex", "Lee"}, {"Sam", "Kim"}, {"Jo", "Max"}}

// startGoBotTeams starts a game with the lobby settings and a team for each
// list of levels, Blue first, with a player for each level, whom the backend
// plays with the default bot of that level, or 0 for a player left to their
// client. It returns each team's players; the game's blue and red are the
// first players of the first two teams.
func startGoBotTeams(t *testing.T, settings string, teams ...[]uint) (goBotGame, [][]domain.Principal) {
	t.Helper()
	database := testutils.InitTestDB(t)
	clock := time.Now()
	game := goBotGame{database: database, publisher: &testutils.RecordedPublisher{}, lobbyID: uuid.NewString(), clock: &clock}
	game.service = &services.GenerationService{Database: database, Publisher: game.publisher, Now: func() time.Time { return *game.clock }}
	snapshot := domain.Snapshot{Lobby: domain.Lobby{ID: game.lobbyID, GameKey: "island-battles", Phase: domain.PhaseStarted, GameSettings: json.RawMessage(settings)}}
	levels := map[string]uint{}
	players := make([][]domain.Principal, len(teams))
	for index, teamLevels := range teams {
		team := domain.Team{ID: uuid.NewString(), Order: index + 1, Name: deadlineTeamNames[index]}
		snapshot.Teams = append(snapshot.Teams, team)
		for seat, level := range teamLevels {
			player := domain.Principal{PlayerID: uuid.NewString()}
			players[index] = append(players[index], player)
			snapshot.StartRoster = append(snapshot.StartRoster, domain.StartRosterEntry{PlayerID: player.PlayerID, TeamID: team.ID})
			snapshot.Members = append(snapshot.Members, domain.Member{PlayerID: player.PlayerID, DisplayName: goBotPlayerNames[index][seat]})
			if level != 0 {
				levels[player.PlayerID] = level
			}
		}
	}
	game.blue, game.red = players[0][0], players[1][0]
	binding, err := services.RunLobbyGameWithGoBots(context.Background(), database, game.publisher, snapshot, levels)
	if err != nil {
		t.Fatal(err)
	}
	game.gameID = binding.GameID
	return game, players
}

func (game goBotGame) current(t *testing.T, player domain.Principal) models.GenerationState {
	t.Helper()
	state, err := game.service.Current(context.Background(), player, game.lobbyID)
	if err != nil {
		t.Fatal(err)
	}
	return state
}

func (game goBotGame) generations(t *testing.T) int64 {
	t.Helper()
	count, err := dal.CountGenerationsForGame(game.database, game.gameID)
	if err != nil {
		t.Fatal(err)
	}
	return count
}

// lopsidedSettings is a map where Blue's 60 soldiers face Red's 5, two
// units apart, so a bot that attacks wins within a few turns.
const lopsidedSettings = `{"turn_timeout_seconds":30,"map":{"map_name":"Lopsided","player_amount":2,"plane_size_x":8,"plane_size_y":6,"neutral_starting_soldier_amount":5,` +
	`"castles":[{"team_color":1,"x":-1,"y":0,"starting_soldier_amount":60},{"team_color":2,"x":1,"y":0}]}}`

func TestGoBotsPlayAGameToItsOutcomeWithoutAnyClient(t *testing.T) {
	game := startGoBotGame(t, lopsidedSettings, 3, 1)
	// Starting the game played it to the end.
	saved, err := dal.FindLobbyGame(game.database, game.lobbyID)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Outcome == nil || saved.Outcome.Reason != models.GameOutcomeLastTeamStanding || *saved.Outcome.WinnerTeamColor != 1 {
		t.Fatalf("unexpected outcome %+v", saved.Outcome)
	}
	generations := game.generations(t)
	// Every generation after the first was pushed to both players, and the
	// last one as game.ended.
	if events, ended := game.publisher.GenerationEvents(t), game.publisher.GameEndedEvents(t); len(events) != 2*int(generations-1) || len(ended) != 2 {
		t.Fatalf("pushed %d generations and %d endings for %d generations", len(events), len(ended), generations)
	}
	state := game.current(t, game.blue)
	if state.Outcome == nil || state.GenerationID != game.publisher.GameEndedEvents(t)[0].State.GenerationID {
		t.Fatalf("unexpected final state %+v", state)
	}
	// Level 3 remembers its turns.
	if !strings.HasPrefix(string(state.Memory), `{"turn":`) {
		t.Fatalf("unexpected memory %s", state.Memory)
	}
	// Each bot logged its first turn, which only its team sees while the
	// game runs and everyone once it has ended.
	stored, err := dal.LoadGameGenerations(game.database, game.gameID)
	if err != nil {
		t.Fatal(err)
	}
	logs, err := game.service.Logs(context.Background(), game.red, game.lobbyID, stored[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	loggers := []string{}
	for _, player := range logs.Players {
		for _, line := range player.Lines {
			if len(loggers) == 0 || loggers[len(loggers)-1] != line.Logger {
				loggers = append(loggers, line.Logger)
			}
		}
	}
	if strings.Join(loggers, " ") != "island_battles.bots.level3 island_battles.bots.level1" {
		t.Fatalf("unexpected game log %+v", logs)
	}
}

func TestGoBotsPlayOnInLaterRequestsAfterTheTimeBudget(t *testing.T) {
	services.SetGoBotTimeBudgets(t, 0, 0)
	game := startGoBotGame(t, `{"turn_timeout_seconds":5}`, 2, 1)
	// Each request plays one more turn, also once the deadline of the turn
	// that the last one left has passed: the bots completed it in time. The
	// turn is open although every roster player has completed it, which
	// isn't an error with Go bots.
	for generations := int64(2); generations <= 5; generations++ {
		if got := game.generations(t); got != generations {
			t.Fatalf("got %d generations, want %d", got, generations)
		}
		*game.clock = game.clock.Add(time.Minute)
		state, err := game.service.ApplyDeadline(context.Background(), game.blue, game.lobbyID)
		if err != nil {
			t.Fatal(err)
		}
		if state.Outcome != nil || !state.Submitted {
			t.Fatalf("unexpected state %+v", state)
		}
	}
}

func TestAHumanAgainstAGoBotPlaysAsSoonAsTheHumanCompletes(t *testing.T) {
	game := startGoBotGame(t, `{"turn_timeout_seconds":30}`, 0, 1)
	alex, sam := game.current(t, game.blue), game.current(t, game.red)
	if alex.Submitted || !sam.Submitted || alex.GenerationID != sam.GenerationID {
		t.Fatalf("the bot didn't play the first turn: %+v %+v", alex, sam)
	}
	completed, err := game.service.Register(context.Background(), game.blue, batch(alex, true))
	if err != nil {
		t.Fatal(err)
	}
	if completed.GenerationID == alex.GenerationID || completed.Submitted {
		t.Fatalf("the turn didn't advance: %+v", completed)
	}
	// The bot sent its soldier, and played the new generation at once.
	if groups := completed.Generation.SoldierGroups; len(groups) != 1 || groups[0].TeamColor != 2 || groups[0].Soldiers != 1 {
		t.Fatalf("unexpected soldier groups %+v", groups)
	}
	sam = game.current(t, game.red)
	if sam.GenerationID != completed.GenerationID || !sam.Submitted {
		t.Fatalf("the bot didn't play the second turn: %+v", sam)
	}
	// The start and the turn pushed their generations, where the bot has
	// completed its turn.
	events := game.publisher.GenerationEvents(t)
	if len(events) != 4 {
		t.Fatalf("got %d events, want 4", len(events))
	}
	for _, event := range events {
		if event.State.Submitted != (event.State.PlayerID == game.red.PlayerID) {
			t.Fatalf("unexpected event %+v", event)
		}
	}
	// Only the human can miss the deadline.
	*game.clock = completed.TurnDeadlineAt.Add(time.Second)
	ended, err := game.service.ApplyDeadline(context.Background(), game.red, game.lobbyID)
	if err != nil {
		t.Fatal(err)
	}
	if ended.Outcome == nil || ended.Outcome.Reason != models.GameOutcomeTurnTimeout || *ended.Outcome.WinnerTeamColor != 2 ||
		ended.Outcome.Summary != "Blue lost: Alex's bot didn't answer within 30 s; Red wins" {
		t.Fatalf("unexpected outcome %+v", ended.Outcome)
	}
}

func TestAGoBotThatFailsPassesItsTurn(t *testing.T) {
	// No default bot has level 9.
	game := startGoBotGame(t, `{}`, 0, 9)
	sam := game.current(t, game.red)
	if !sam.Submitted || sam.Memory != nil {
		t.Fatalf("the failed bot didn't pass: %+v", sam)
	}
	logs, err := game.service.Logs(context.Background(), game.red, game.lobbyID, sam.GenerationID)
	if err != nil {
		t.Fatal(err)
	}
	if len(logs.Players) != 1 || len(logs.Players[0].Lines) != 1 {
		t.Fatalf("unexpected game log %+v", logs)
	}
	if line := logs.Players[0].Lines[0]; line.Level != models.GenerationLogError || line.Logger != services.GoBotLogger ||
		line.Message != "The level 9 bot failed this turn, so it passes: there is no default bot of level 9" {
		t.Fatalf("unexpected line %+v", line)
	}
	completed, err := game.service.Register(context.Background(), game.blue, batch(game.current(t, game.blue), true))
	if err != nil {
		t.Fatal(err)
	}
	if completed.GenerationID == sam.GenerationID || len(completed.Generation.SoldierGroups) != 0 {
		t.Fatalf("unexpected state %+v", completed)
	}
}

func TestAGoBotAssignedDuringATurnPlaysBeforeTheDeadline(t *testing.T) {
	game := startGoBotGame(t, `{"turn_timeout_seconds":30}`, 0, 0)
	alex := game.current(t, game.blue)
	if _, err := game.service.Register(context.Background(), game.blue, batch(alex, true)); err != nil {
		t.Fatal(err)
	}
	testutils.AssignGoBotForTest(t, game.database, game.gameID, game.red.PlayerID, 2)
	*game.clock = *alex.TurnDeadlineAt
	state, err := game.service.ApplyDeadline(context.Background(), game.blue, game.lobbyID)
	if err != nil {
		t.Fatal(err)
	}
	if state.Outcome != nil || state.GenerationID == alex.GenerationID {
		t.Fatalf("the bot didn't play in time: %+v", state)
	}
}

// islandsSettings is the tournament's "islands" map: Blue's and Red's castles
// of 30 soldiers, at (-4, 0) and (4, 0), and three pairs of mirrored neutral
// castles between them.
const islandsSettings = `{"turn_timeout_seconds":30,"map":{"map_name":"Islands","player_amount":2,"plane_size_x":10,"plane_size_y":8,"neutral_starting_soldier_amount":10,` +
	`"castles":[{"team_color":1,"x":-4,"y":0,"starting_soldier_amount":30},{"team_color":2,"x":4,"y":0,"starting_soldier_amount":30},` +
	`{"team_color":0,"x":-2.5,"y":2,"starting_soldier_amount":10},{"team_color":0,"x":2.5,"y":-2,"starting_soldier_amount":10},` +
	`{"team_color":0,"x":-1,"y":1.5,"starting_soldier_amount":15},{"team_color":0,"x":1,"y":-1.5,"starting_soldier_amount":15},` +
	`{"team_color":0,"x":-2.5,"y":-2.5,"starting_soldier_amount":20},{"team_color":0,"x":2.5,"y":2.5,"starting_soldier_amount":20}]}}`

func TestTeammatesGoBotsSendOnlyWhatTheOthersLeft(t *testing.T) {
	// Each team's bots used to see the same castles, so the second planned
	// with the soldiers the first had already sent, and its whole turn
	// failed. The game plays to its end.
	game, players := startGoBotTeams(t, islandsSettings, []uint{2, 2}, []uint{3, 3})
	for state := game.current(t, game.blue); state.Outcome == nil; {
		var err error
		if state, err = game.service.ApplyDeadline(context.Background(), game.blue, game.lobbyID); err != nil {
			t.Fatal(err)
		}
	}
	generations, err := dal.LoadGameGenerations(game.database, game.gameID)
	if err != nil {
		t.Fatal(err)
	}
	// No bot failed a turn, and each team's bots sent commands in the same
	// turn.
	together := map[int]bool{}
	for _, generation := range generations {
		lines, err := dal.LoadGenerationLogs(game.database, generation.ID)
		if err != nil {
			t.Fatal(err)
		}
		for _, line := range lines {
			if line.Level == models.GenerationLogError {
				t.Errorf("a bot failed generation %d: %s", generation.ID, line.Message)
			}
		}
		inputs, err := dal.LoadGenerationInputs(game.database, generation.ID)
		if err != nil {
			t.Fatal(err)
		}
		sent := map[string]int{}
		for _, input := range inputs {
			var commands []models.CommandFields
			if err := json.Unmarshal(input.Commands, &commands); err != nil {
				t.Fatal(err)
			}
			sent[input.PlayerID] = len(commands)
		}
		for index, team := range players {
			if sent[team[0].PlayerID] > 0 && sent[team[1].PlayerID] > 0 {
				together[index] = true
			}
		}
	}
	if !together[0] || !together[1] {
		t.Fatalf("not every team's bots sent commands in the same turn: %v", together)
	}
}

func TestReadsPlayGoBotTurnsForLessTimeThanWrites(t *testing.T) {
	services.SetGoBotTimeBudgets(t, 0, 0)
	game := startGoBotGame(t, `{"turn_timeout_seconds":30}`, 2, 1)
	ctx := context.Background()
	// With a long write budget, each read still plays a single turn.
	services.SetGoBotTimeBudgets(t, time.Minute, 0)
	reads := []func() error{
		func() error { _, err := game.service.Current(ctx, game.blue, game.lobbyID); return err },
		func() error { _, err := game.service.Replay(ctx, game.blue, game.lobbyID); return err },
		func() error {
			state := game.current(t, game.blue)
			_, err := game.service.Logs(ctx, game.blue, game.lobbyID, state.GenerationID)
			return err
		},
		func() error { _, err := game.service.Demo(ctx, game.blue, game.lobbyID); return err },
	}
	for index, read := range reads {
		before := game.generations(t)
		if err := read(); err != nil {
			t.Fatal(err)
		}
		// The logs read reads the state first.
		want := int64(1)
		if index == 2 {
			want = 2
		}
		if got := game.generations(t) - before; got != want {
			t.Fatalf("read %d played %d turns, want %d", index, got, want)
		}
	}
	// The deadline call plays with the write budget, here to the end.
	state, err := game.service.ApplyDeadline(ctx, game.blue, game.lobbyID)
	if err != nil {
		t.Fatal(err)
	}
	if state.Outcome == nil {
		t.Fatalf("the deadline call didn't play the game to its end: %d generations", game.generations(t))
	}
}
