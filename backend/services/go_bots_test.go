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
	database := testutils.InitTestDB(t)
	clock := time.Now()
	game := goBotGame{database: database, publisher: &testutils.RecordedPublisher{}, lobbyID: uuid.NewString(), clock: &clock,
		blue: domain.Principal{PlayerID: uuid.NewString()}, red: domain.Principal{PlayerID: uuid.NewString()}}
	game.service = &services.GenerationService{Database: database, Publisher: game.publisher, Now: func() time.Time { return *game.clock }}
	snapshot := domain.Snapshot{Lobby: domain.Lobby{ID: game.lobbyID, GameKey: "island-battles", Phase: domain.PhaseStarted, GameSettings: json.RawMessage(settings)}}
	levels := map[string]uint{}
	for index, player := range []struct {
		principal domain.Principal
		name      string
		level     uint
	}{{game.blue, "Alex", blueLevel}, {game.red, "Sam", redLevel}} {
		team := domain.Team{ID: uuid.NewString(), Order: index + 1, Name: deadlineTeamNames[index]}
		snapshot.Teams = append(snapshot.Teams, team)
		snapshot.StartRoster = append(snapshot.StartRoster, domain.StartRosterEntry{PlayerID: player.principal.PlayerID, TeamID: team.ID})
		snapshot.Members = append(snapshot.Members, domain.Member{PlayerID: player.principal.PlayerID, DisplayName: player.name})
		if player.level != 0 {
			levels[player.principal.PlayerID] = player.level
		}
	}
	binding, err := services.RunLobbyGameWithGoBots(context.Background(), database, game.publisher, snapshot, levels)
	if err != nil {
		t.Fatal(err)
	}
	game.gameID = binding.GameID
	return game
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
	services.SetGoBotTimeBudget(t, 0)
	game := startGoBotGame(t, `{"turn_timeout_seconds":5}`, 2, 1)
	// Each request plays one more turn, also once the deadline of the turn
	// that the last one left has passed: the bots completed it in time.
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
