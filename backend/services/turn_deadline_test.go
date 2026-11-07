package services_test

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"sync"
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

// deadlineFixture is a started game whose service runs on a clock the test
// sets. players[team][index] are the roster players by team, in colour
// order, and states holds each player's first state.
type deadlineFixture struct {
	database  *gorm.DB
	publisher *testutils.RecordedPublisher
	service   *services.GenerationService
	lobbyID   string
	clock     *time.Time
	players   [][]domain.Principal
	states    map[string]models.GenerationState
}

var deadlineTeamNames = []string{"Blue", "Red", "Green"}

// mapSettings writes lobby settings with the turn timeout, as JSON, and a map
// that gives each team colour in castleTeams a castle, 0 for neutral.
func mapSettings(timeout string, castleTeams ...uint) string {
	teams := map[uint]bool{}
	castles := []string{}
	for index, team := range castleTeams {
		if team != models.NeutralTeamColor {
			teams[team] = true
		}
		castles = append(castles, fmt.Sprintf(`{"team_color":%d,"x":%g,"y":0,"starting_soldier_amount":30}`, team, -3+0.5*float64(index)))
	}
	return fmt.Sprintf(`{"turn_timeout_seconds":%s,"map":{"map_name":"Deadline","player_amount":%d,"plane_size_x":8,"plane_size_y":6,"neutral_starting_soldier_amount":5,"castles":[%s]}}`,
		timeout, len(teams), strings.Join(castles, ","))
}

// newDeadlineFixture starts a game with the lobby settings, empty for none,
// and teams with the players named in each, so Blue has names[0].
func newDeadlineFixture(t *testing.T, settings string, names ...[]string) deadlineFixture {
	t.Helper()
	database := testutils.InitTestDB(t)
	fixture := deadlineFixture{database: database, publisher: &testutils.RecordedPublisher{}, lobbyID: uuid.NewString(), states: map[string]models.GenerationState{}}
	snapshot := domain.Snapshot{Lobby: domain.Lobby{ID: fixture.lobbyID, GameKey: "island-battles", Phase: domain.PhaseStarted, GameSettings: json.RawMessage(settings)}}
	for index, teamNames := range names {
		team := domain.Team{ID: uuid.NewString(), Order: index + 1, Name: deadlineTeamNames[index]}
		snapshot.Teams = append(snapshot.Teams, team)
		players := []domain.Principal{}
		for _, name := range teamNames {
			player := domain.Principal{PlayerID: uuid.NewString()}
			players = append(players, player)
			snapshot.StartRoster = append(snapshot.StartRoster, domain.StartRosterEntry{PlayerID: player.PlayerID, TeamID: team.ID})
			snapshot.Members = append(snapshot.Members, domain.Member{PlayerID: player.PlayerID, DisplayName: name})
		}
		fixture.players = append(fixture.players, players)
	}
	if _, err := services.RunLobbyGame(context.Background(), database, fixture.publisher, snapshot); err != nil {
		t.Fatal(err)
	}
	clock := time.Now()
	fixture.clock = &clock
	fixture.service = &services.GenerationService{Database: database, Publisher: fixture.publisher, Now: func() time.Time { return *fixture.clock }}
	for _, team := range fixture.players {
		for _, player := range team {
			fixture.states[player.PlayerID] = fixture.current(t, player)
		}
	}
	return fixture
}

func (fixture deadlineFixture) current(t *testing.T, player domain.Principal) models.GenerationState {
	t.Helper()
	state, err := fixture.service.Current(context.Background(), player, fixture.lobbyID)
	if err != nil {
		t.Fatal(err)
	}
	return state
}

// complete completes the player's turn with no commands.
func (fixture deadlineFixture) complete(t *testing.T, player domain.Principal) models.GenerationState {
	t.Helper()
	state, err := fixture.service.Register(context.Background(), player, batch(fixture.states[player.PlayerID], true))
	if err != nil {
		t.Fatal(err)
	}
	return state
}

// deadline is the first generation's deadline, which every player's state
// carries.
func (fixture deadlineFixture) deadline(t *testing.T) time.Time {
	t.Helper()
	state := fixture.states[fixture.players[0][0].PlayerID]
	if state.TurnDeadlineAt == nil {
		t.Fatal("the turn has no deadline")
	}
	return *state.TurnDeadlineAt
}

// applyDeadline applies the deadline as the player, and returns their state.
func (fixture deadlineFixture) applyDeadline(t *testing.T, player domain.Principal) models.GenerationState {
	t.Helper()
	state, err := fixture.service.ApplyDeadline(context.Background(), player, fixture.lobbyID)
	if err != nil {
		t.Fatal(err)
	}
	return state
}

func (fixture deadlineFixture) savedOutcome(t *testing.T) *models.GameOutcome {
	t.Helper()
	game, err := dal.FindLobbyGame(fixture.database, fixture.lobbyID)
	if err != nil {
		t.Fatal(err)
	}
	return game.Outcome
}

func colour(color uint) *uint { return &color }

func TestTurnDeadlineIsTheGenerationsCreationPlusTheTimeout(t *testing.T) {
	fixture := newDeadlineFixture(t, `{"turn_timeout_seconds":120}`, []string{"Alex"}, []string{"Sam"})
	generation, err := dal.GetGeneration(fixture.database, fixture.states[fixture.players[0][0].PlayerID].GenerationID)
	if err != nil {
		t.Fatal(err)
	}
	if deadline := fixture.deadline(t); !deadline.Equal(generation.CreatedAt.Add(120 * time.Second)) {
		t.Fatalf("deadline = %v, want %v", deadline, generation.CreatedAt.Add(120*time.Second))
	}
	// Every generation has its own deadline, also in the pushed events.
	fixture.complete(t, fixture.players[0][0])
	next := fixture.complete(t, fixture.players[1][0])
	generation, err = dal.GetGeneration(fixture.database, next.GenerationID)
	if err != nil {
		t.Fatal(err)
	}
	if next.TurnDeadlineAt == nil || !next.TurnDeadlineAt.Equal(generation.CreatedAt.Add(120*time.Second)) {
		t.Fatalf("next deadline = %v, created %v", next.TurnDeadlineAt, generation.CreatedAt)
	}
	events := fixture.publisher.GenerationEvents(t)
	if pushed := events[len(events)-1].State.TurnDeadlineAt; pushed == nil || !pushed.Equal(*next.TurnDeadlineAt) {
		t.Fatalf("pushed deadline = %v, want %v", pushed, next.TurnDeadlineAt)
	}

	// Lobbies that set no timeout get 30 seconds.
	defaults := newDeadlineFixture(t, ``, []string{"Alex"}, []string{"Sam"})
	generation, err = dal.GetGeneration(defaults.database, defaults.states[defaults.players[0][0].PlayerID].GenerationID)
	if err != nil {
		t.Fatal(err)
	}
	if deadline := defaults.deadline(t); !deadline.Equal(generation.CreatedAt.Add(30 * time.Second)) {
		t.Fatalf("default deadline = %v, created %v", deadline, generation.CreatedAt)
	}
}

func TestNothingTimesOutBeforeTheDeadline(t *testing.T) {
	fixture := newDeadlineFixture(t, `{"turn_timeout_seconds":10}`, []string{"Alex"}, []string{"Sam"})
	alex, sam := fixture.players[0][0], fixture.players[1][0]
	fixture.complete(t, alex)
	*fixture.clock = fixture.deadline(t).Add(-time.Millisecond)
	pushed := len(fixture.publisher.Publications())
	if state := fixture.applyDeadline(t, alex); state.Outcome != nil || !state.Submitted || state.TurnDeadlineAt == nil {
		t.Fatalf("state before the deadline = %+v", state)
	}
	if _, err := fixture.service.Replay(context.Background(), sam, fixture.lobbyID); err != nil {
		t.Fatal(err)
	}
	if fixture.savedOutcome(t) != nil || len(fixture.publisher.Publications()) != pushed {
		t.Fatal("the game ended before its deadline")
	}
	// A batch just before the deadline plays the turn.
	if next := fixture.complete(t, sam); next.Outcome != nil || next.GenerationID == fixture.states[sam.PlayerID].GenerationID {
		t.Fatalf("the last completion before the deadline = %+v", next)
	}
}

func TestTheIncompletePlayersTeamLosesAtTheDeadline(t *testing.T) {
	fixture := newDeadlineFixture(t, `{"turn_timeout_seconds":30}`, []string{"Alex"}, []string{"Sam"})
	alex, sam := fixture.players[0][0], fixture.players[1][0]
	first := fixture.states[alex.PlayerID]
	fixture.complete(t, alex)
	*fixture.clock = fixture.deadline(t).Add(time.Second)
	pushed := len(fixture.publisher.Publications())

	state := fixture.applyDeadline(t, alex)
	want := models.GameOutcomeView{Reason: models.GameOutcomeTurnTimeout, WinnerTeamColor: colour(1), Summary: "Red lost: Sam's bot didn't answer within 30 s; Blue wins"}
	if !reflect.DeepEqual(state.Outcome, &want) || state.GenerationID != first.GenerationID || !state.Submitted || state.TurnDeadlineAt != nil {
		t.Fatalf("state after the deadline = %+v, outcome %+v", state, state.Outcome)
	}
	saved := models.GameOutcome{Reason: models.GameOutcomeTurnTimeout, WinnerTeamColor: colour(1), TimedOut: []models.TimedOutTeam{{Color: 2, Players: []string{"Sam"}}}}
	if outcome := fixture.savedOutcome(t); !reflect.DeepEqual(outcome, &saved) {
		t.Fatalf("saved outcome = %+v", outcome)
	}
	// The open generation is pushed once more, as the game's end, with each
	// player's own completion.
	ended := fixture.publisher.GameEndedEvents(t)
	if len(fixture.publisher.Publications()) != pushed+2 || len(ended) != 2 {
		t.Fatalf("pushed %d events, %d of them game.ended", len(fixture.publisher.Publications())-pushed, len(ended))
	}
	for _, event := range ended {
		if event.State.GenerationID != first.GenerationID || !reflect.DeepEqual(event.State.Outcome, &want) || event.State.Submitted != (event.State.PlayerID == alex.PlayerID) || event.State.TurnDeadlineAt != nil {
			t.Fatalf("bad game.ended event: %+v", event.State)
		}
	}

	// Applying it again, or any later request, ends nothing more.
	if again := fixture.applyDeadline(t, sam); !reflect.DeepEqual(again.Outcome, &want) || again.Submitted {
		t.Fatalf("second application = %+v", again)
	}
	*fixture.clock = fixture.clock.Add(time.Hour)
	if read := fixture.current(t, alex); !reflect.DeepEqual(read.Outcome, &want) {
		t.Fatalf("read = %+v", read)
	}
	if replay, err := fixture.service.Replay(context.Background(), sam, fixture.lobbyID); err != nil || !reflect.DeepEqual(replay.Outcome, &want) || len(replay.Frames) != 1 {
		t.Fatalf("replay = %+v, %v", replay.Outcome, err)
	}
	// Sam's late batch for the expired generation conflicts.
	_, err := fixture.service.Register(context.Background(), sam, batch(first, true))
	assertGameEnded(t, err)
	if generations, err := dal.CountGenerationsForGame(fixture.database, first.GameID); err != nil || generations != 1 {
		t.Fatalf("generations = %d, %v", generations, err)
	}
	if len(fixture.publisher.Publications()) != pushed+2 {
		t.Fatal("the game ended more than once")
	}
}

func TestALateBatchAppliesTheDeadlineAndConflicts(t *testing.T) {
	fixture := newDeadlineFixture(t, `{"turn_timeout_seconds":10}`, []string{"Alex"}, []string{"Sam"})
	alex, sam := fixture.players[0][0], fixture.players[1][0]
	first := fixture.states[sam.PlayerID]
	// Sam's first batch is in time, and can be retried after the deadline.
	inTime := batch(first, false)
	if _, err := fixture.service.Register(context.Background(), sam, inTime); err != nil {
		t.Fatal(err)
	}
	*fixture.clock = fixture.deadline(t).Add(time.Second)
	// No request applied the deadline yet; the late batch does, and the
	// game's end commits although the batch is rejected.
	_, err := fixture.service.Register(context.Background(), sam, batch(first, true))
	assertGameEnded(t, err)
	_, err = fixture.service.Register(context.Background(), alex, batch(first, true, send(0, 1, 5)))
	assertGameEnded(t, err)
	if outcome := fixture.savedOutcome(t); outcome == nil || outcome.Reason != models.GameOutcomeTurnTimeout || outcome.WinnerTeamColor != nil || len(outcome.TimedOut) != 2 {
		t.Fatalf("saved outcome = %+v", outcome)
	}
	if ended := fixture.publisher.GameEndedEvents(t); len(ended) != 2 {
		t.Fatalf("pushed %d game.ended events", len(ended))
	}
	if retried, err := fixture.service.Register(context.Background(), sam, inTime); err != nil || retried.Outcome == nil {
		t.Fatalf("retry of an earlier batch = %+v, %v", retried, err)
	}
	if state := fixture.current(t, alex); state.Outcome == nil || state.Outcome.Summary != "Blue lost: Alex's bot didn't answer within 10 s; Red lost: Sam's bot didn't answer within 10 s" {
		t.Fatalf("outcome = %+v", state.Outcome)
	}
}

func TestTheMostCastlesWinAmongTheTeamsThatAnswered(t *testing.T) {
	players := [][]string{{"Alex"}, {"Sam"}, {"Kim"}}
	for _, scenario := range []struct {
		name     string
		castles  []uint
		answered []int
		winner   *uint
		timedOut []models.TimedOutTeam
		summary  string
	}{
		{"most castles", []uint{1, 1, 1, 2, 3, 3, 0}, []int{1, 2}, colour(3), []models.TimedOutTeam{{Color: 1, Players: []string{"Alex"}}},
			"Blue lost: Alex's bot didn't answer within 2 min; Green wins"},
		{"equal castle counts draw", []uint{1, 1, 1, 2, 3, 0}, []int{1, 2}, nil, []models.TimedOutTeam{{Color: 1, Players: []string{"Alex"}}},
			"Blue lost: Alex's bot didn't answer within 2 min; the other teams draw"},
		{"two teams time out together", []uint{1, 1, 2, 2, 3}, []int{2}, colour(3), []models.TimedOutTeam{{Color: 1, Players: []string{"Alex"}}, {Color: 2, Players: []string{"Sam"}}},
			"Blue lost: Alex's bot didn't answer within 2 min; Red lost: Sam's bot didn't answer within 2 min; Green wins"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			fixture := newDeadlineFixture(t, mapSettings("120", scenario.castles...), players...)
			for _, team := range scenario.answered {
				fixture.complete(t, fixture.players[team][0])
			}
			*fixture.clock = fixture.deadline(t)
			state := fixture.applyDeadline(t, fixture.players[2][0])
			want := models.GameOutcome{Reason: models.GameOutcomeTurnTimeout, WinnerTeamColor: scenario.winner, TimedOut: scenario.timedOut}
			if outcome := fixture.savedOutcome(t); !reflect.DeepEqual(outcome, &want) {
				t.Fatalf("outcome = %+v, want %+v", outcome, want)
			}
			if state.Outcome == nil || state.Outcome.Summary != scenario.summary || !reflect.DeepEqual(state.Outcome.WinnerTeamColor, scenario.winner) {
				t.Fatalf("state outcome = %+v", state.Outcome)
			}
		})
	}
}

func TestATeamLosesForEachPlayerWhoDidNotAnswer(t *testing.T) {
	fixture := newDeadlineFixture(t, `{"turn_timeout_seconds":5}`, []string{"Kim", "Alex", "Lee"}, []string{"Sam"})
	// Kim and Sam answer; Alex and Lee don't, so Blue loses.
	fixture.complete(t, fixture.players[0][0])
	fixture.complete(t, fixture.players[1][0])
	*fixture.clock = fixture.deadline(t).Add(time.Minute)
	state := fixture.applyDeadline(t, fixture.players[1][0])
	if state.Outcome == nil || state.Outcome.Summary != "Blue lost: Alex's and Lee's bots didn't answer within 5 s; Red wins" {
		t.Fatalf("outcome = %+v", state.Outcome)
	}
}

func TestApplyingTheDeadlineTwiceEndsTheGameOnce(t *testing.T) {
	fixture := newDeadlineFixture(t, `{"turn_timeout_seconds":5}`, []string{"Alex"}, []string{"Sam"})
	*fixture.clock = fixture.deadline(t).Add(time.Second)
	var wait sync.WaitGroup
	for range 4 {
		for _, team := range fixture.players {
			wait.Add(1)
			go func() {
				defer wait.Done()
				if _, err := fixture.service.ApplyDeadline(context.Background(), team[0], fixture.lobbyID); err != nil {
					t.Error(err)
				}
			}()
		}
	}
	wait.Wait()
	if ended := fixture.publisher.GameEndedEvents(t); len(ended) != 2 {
		t.Fatalf("pushed %d game.ended events, want one per player", len(ended))
	}
	if outcome := fixture.savedOutcome(t); outcome == nil || outcome.Reason != models.GameOutcomeTurnTimeout {
		t.Fatalf("outcome = %+v", outcome)
	}
}

func TestANullTimeoutNeverTimesOut(t *testing.T) {
	fixture := newDeadlineFixture(t, `{"turn_timeout_seconds":null}`, []string{"Alex"}, []string{"Sam"})
	alex, sam := fixture.players[0][0], fixture.players[1][0]
	if state := fixture.states[alex.PlayerID]; state.TurnDeadlineAt != nil {
		t.Fatalf("deadline = %v, want none", state.TurnDeadlineAt)
	}
	fixture.complete(t, alex)
	*fixture.clock = fixture.clock.Add(365 * 24 * time.Hour)
	if state := fixture.applyDeadline(t, alex); state.Outcome != nil || state.TurnDeadlineAt != nil {
		t.Fatalf("state a year later = %+v", state)
	}
	if _, err := fixture.service.Logs(context.Background(), sam, fixture.lobbyID, fixture.states[sam.PlayerID].GenerationID); err != nil {
		t.Fatal(err)
	}
	if next := fixture.complete(t, sam); next.Outcome != nil || next.GenerationID == fixture.states[sam.PlayerID].GenerationID || next.TurnDeadlineAt != nil {
		t.Fatalf("the turn did not go on: %+v", next)
	}
	if len(fixture.publisher.GameEndedEvents(t)) != 0 {
		t.Fatal("a game without a timeout ended")
	}
}

func TestReadsWithoutTheGameLockApplyTheDeadline(t *testing.T) {
	for name, read := range map[string]func(deadlineFixture, domain.Principal) error{
		"replay": func(fixture deadlineFixture, player domain.Principal) error {
			replay, err := fixture.service.Replay(context.Background(), player, fixture.lobbyID)
			if err == nil && replay.Outcome == nil {
				err = fmt.Errorf("the replay has no outcome")
			}
			return err
		},
		"logs": func(fixture deadlineFixture, player domain.Principal) error {
			logs, err := fixture.service.Logs(context.Background(), player, fixture.lobbyID, fixture.states[player.PlayerID].GenerationID)
			// Once the game has ended, every team's lines are visible.
			if err == nil && len(logs.HiddenTeamColors) != 0 {
				err = fmt.Errorf("hidden teams %v after the end", logs.HiddenTeamColors)
			}
			return err
		},
		"demo": func(fixture deadlineFixture, player domain.Principal) error {
			demo, err := fixture.service.Demo(context.Background(), player, fixture.lobbyID)
			if err == nil && demo.Outcome == nil {
				err = fmt.Errorf("the demo has no outcome")
			}
			return err
		},
	} {
		t.Run(name, func(t *testing.T) {
			fixture := newDeadlineFixture(t, `{"turn_timeout_seconds":5}`, []string{"Alex"}, []string{"Sam"})
			*fixture.clock = fixture.deadline(t).Add(time.Second)
			if err := read(fixture, fixture.players[0][0]); err != nil {
				t.Fatal(err)
			}
			if len(fixture.publisher.GameEndedEvents(t)) != 2 {
				t.Fatal("the read did not push the game's end")
			}
		})
	}
}
