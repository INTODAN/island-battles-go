package services_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/INTODAN/mpl/domain"
	"islandBattles/backend/dal"
	"islandBattles/backend/internal/testutils"
	"islandBattles/backend/models"
	"islandBattles/backend/retention"
	"islandBattles/backend/services"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// ratedPeople and ratedBots are the people of a rated test game's teams, in
// colour order, and the bot names their localhost bots play as.
var (
	ratedPeople = []string{"Alex", "Sam", "Kim"}
	ratedBots   = []string{"Kraken", "Leviathan", "Hydra"}
)

// ratedFixture is a started game of teams signed-in people, each with a
// localhost bot that plays as their bot name, so the game waits for every
// player. competitors are the bot names, by team in colour order.
type ratedFixture struct {
	deadlineFixture
	competitors []models.CompetitorView
}

// newRatedFixture starts a game of teams teams with the lobby settings,
// rated when rated is set. settings choose the map, which decides the
// number of teams (mapSettings).
func newRatedFixture(t *testing.T, settings string, rated bool, teams int) ratedFixture {
	t.Helper()
	database := testutils.InitTestDB(t)
	fields := map[string]any{}
	if err := json.Unmarshal([]byte(settings), &fields); err != nil {
		t.Fatal(err)
	}
	fields["rated"] = rated
	combined, err := json.Marshal(fields)
	if err != nil {
		t.Fatal(err)
	}
	fixture := ratedFixture{deadlineFixture: deadlineFixture{database: database, publisher: &testutils.RecordedPublisher{}, lobbyID: uuid.NewString(), states: map[string]models.GenerationState{}}}
	snapshot := domain.Snapshot{Lobby: domain.Lobby{ID: fixture.lobbyID, Round: 1, GameKey: "island-battles", Phase: domain.PhaseStarted, GameSettings: combined}}
	accounts := services.Accounts{Database: database}
	for index := range teams {
		accountID := uuid.NewString()
		person := domain.Principal{PlayerID: uuid.NewString(), DisplayName: ratedPeople[index], AccountID: accountID}
		competitor, err := accounts.RegisterBotName(context.Background(), person, ratedBots[index])
		if err != nil {
			t.Fatal(err)
		}
		fixture.competitors = append(fixture.competitors, competitor)
		team := domain.Team{ID: uuid.NewString(), Order: index + 1, Name: deadlineTeamNames[index]}
		snapshot.Teams = append(snapshot.Teams, team)
		snapshot.StartRoster = append(snapshot.StartRoster, domain.StartRosterEntry{PlayerID: person.PlayerID, TeamID: team.ID})
		snapshot.Members = append(snapshot.Members, domain.Member{PlayerID: person.PlayerID, DisplayName: person.DisplayName, AccountID: &accountID, TeamID: &team.ID, Active: true,
			MemberSettings: json.RawMessage(`{"bot":{"kind":"localhost","name":"` + ratedBots[index] + `"}}`)})
		fixture.players = append(fixture.players, []domain.Principal{person})
	}
	if _, err := services.RunLobbyGame(context.Background(), database, fixture.publisher, nil, snapshot); err != nil {
		t.Fatal(err)
	}
	clock := time.Now()
	fixture.clock = &clock
	fixture.service = &services.GenerationService{Database: database, Publisher: fixture.publisher, Now: func() time.Time { return *fixture.clock }}
	for _, team := range fixture.players {
		fixture.states[team[0].PlayerID] = fixture.current(t, team[0])
	}
	return fixture
}

// capSettings are the settings of a game on a map that gives each colour in
// castleTeams a castle, which playFirstTurn ends at the generation cap: the
// team with the most castles wins.
func capSettings(castleTeams ...uint) string { return mapSettings("null", castleTeams...) }

// playFirstTurn has every player complete the first turn, which ends the
// game at the generation cap, and returns the state that the last completion
// answers. The cap is set on the game: a lobby's is at least 50.
func (fixture ratedFixture) playFirstTurn(t *testing.T) models.GenerationState {
	t.Helper()
	gameID := fixture.states[fixture.players[0][0].PlayerID].GameID
	if err := fixture.database.Model(&models.Game{Model: gorm.Model{ID: gameID}}).Select("MaxGenerations").Updates(&models.Game{MaxGenerations: 2}).Error; err != nil {
		t.Fatal(err)
	}
	var state models.GenerationState
	for _, team := range fixture.players {
		state = fixture.complete(t, team[0])
	}
	if state.Outcome == nil {
		t.Fatalf("the first turn didn't end the game: %+v", state)
	}
	return state
}

// competitor reads a competitor's row, soft-deleted or not.
func (fixture ratedFixture) competitor(t *testing.T, id string) models.Competitor {
	t.Helper()
	var competitor models.Competitor
	if err := fixture.database.Unscoped().Where(&models.Competitor{ID: id}).First(&competitor).Error; err != nil {
		t.Fatal(err)
	}
	return competitor
}

// ratingEvents reads the rating history of the fixture's game, by
// competitor.
func (fixture ratedFixture) ratingEvents(t *testing.T) map[string]models.RatingEvent {
	t.Helper()
	gameID := fixture.states[fixture.players[0][0].PlayerID].GameID
	var events []models.RatingEvent
	if err := fixture.database.Where(&models.RatingEvent{GameID: gameID}).Find(&events).Error; err != nil {
		t.Fatal(err)
	}
	byCompetitor := map[string]models.RatingEvent{}
	for _, event := range events {
		byCompetitor[event.CompetitorID] = event
	}
	return byCompetitor
}

// ratingChanges are the rating changes that the outcome should show for the
// fixture's teams, with each team's rating after the game.
func (fixture ratedFixture) ratingChanges(after ...float64) []models.RatingChange {
	changes := []models.RatingChange{}
	for index, rating := range after {
		changes = append(changes, models.RatingChange{TeamColor: uint(index + 1), CompetitorID: fixture.competitors[index].ID, Kind: models.BotCompetitor,
			Name: ratedBots[index], Owner: ratedPeople[index], Before: 1500, After: rating})
	}
	return changes
}

// assertRated checks each team's competitor and history row after the game:
// its rating, its record of one game with result, and the K it played with.
func (fixture ratedFixture) assertRated(t *testing.T, ratings []float64, results []models.RatingResult, ks []int) {
	t.Helper()
	events := fixture.ratingEvents(t)
	if len(events) != len(ratings) {
		t.Fatalf("%d rating history rows, want %d", len(events), len(ratings))
	}
	for index, view := range fixture.competitors {
		competitor := fixture.competitor(t, view.ID)
		wins, draws, losses := 0, 0, 0
		switch results[index] {
		case models.RatingWin:
			wins = 1
		case models.RatingDraw:
			draws = 1
		default:
			losses = 1
		}
		if math.Abs(competitor.Rating-ratings[index]) > 1e-9 || competitor.Games != view.Games+1 || competitor.Wins != wins || competitor.Draws != draws || competitor.Losses != losses || competitor.LastPlayedAt == nil {
			t.Fatalf("%s after the game = %+v, want rating %g and one %s", ratedBots[index], competitor, ratings[index], results[index])
		}
		event := events[view.ID]
		if event.Result != results[index] || event.RatingBefore != view.Rating || math.Abs(event.RatingAfter-ratings[index]) > 1e-9 || event.K != ks[index] || len(event.Opponents) != len(ratings)-1 || event.EndedAt.IsZero() {
			t.Fatalf("%s's history row = %+v, want %s from %g to %g with K %d", ratedBots[index], event, results[index], view.Rating, ratings[index], ks[index])
		}
		for _, opponent := range event.Opponents {
			if opponent.CompetitorID == view.ID || opponent.RatingBefore != 1500 {
				t.Fatalf("%s's opponents = %+v", ratedBots[index], event.Opponents)
			}
		}
	}
}

// Two new competitors play a rated game to its end: the winner is at 1520
// and the loser at 1480, each with one game, and the outcome says so to the
// players, in game.ended and in a demo, as watchers see it too.
func TestARatedGameMovesTwoNewCompetitorsTo1520And1480(t *testing.T) {
	fixture := newRatedFixture(t, capSettings(1, 1, 2), true, 2)
	state := fixture.playFirstTurn(t)
	want := fixture.ratingChanges(1520, 1480)
	if state.Outcome.Reason != models.GameOutcomeGenerationCap || state.Outcome.WinnerTeamColor == nil || *state.Outcome.WinnerTeamColor != 1 || !reflect.DeepEqual(state.Outcome.Ratings, want) {
		t.Fatalf("outcome = %+v, want Blue to win with ratings %+v", state.Outcome, want)
	}
	fixture.assertRated(t, []float64{1520, 1480}, []models.RatingResult{models.RatingWin, models.RatingLoss}, []int{40, 40})

	if saved := fixture.savedOutcome(t); !reflect.DeepEqual(saved.Ratings, want) {
		t.Fatalf("stored ratings = %+v", saved.Ratings)
	}
	ended := fixture.publisher.GameEndedEvents(t)
	if len(ended) != 2 || !reflect.DeepEqual(ended[0].State.Outcome.Ratings, want) {
		t.Fatalf("game.ended = %+v", ended)
	}
	ctx := context.Background()
	alex := fixture.players[0][0]
	demo, err := fixture.service.Demo(ctx, alex, fixture.lobbyID)
	if err != nil || demo.Outcome == nil || !reflect.DeepEqual(demo.Outcome.Ratings, want) {
		t.Fatalf("demo = %+v, %v", demo.Outcome, err)
	}
	// Reading the game again rates nothing again.
	if again := fixture.current(t, alex); !reflect.DeepEqual(again.Outcome.Ratings, want) {
		t.Fatalf("state read again = %+v", again.Outcome)
	}
	fixture.assertRated(t, []float64{1520, 1480}, []models.RatingResult{models.RatingWin, models.RatingLoss}, []int{40, 40})
}

// A drawn game between two new competitors changes no rating and counts a
// draw for both.
func TestARatedDrawBetweenEqualRatingsChangesNoRating(t *testing.T) {
	fixture := newRatedFixture(t, capSettings(1, 2), true, 2)
	state := fixture.playFirstTurn(t)
	want := fixture.ratingChanges(1500, 1500)
	if state.Outcome.WinnerTeamColor != nil || !reflect.DeepEqual(state.Outcome.Ratings, want) {
		t.Fatalf("outcome = %+v, want a draw with ratings %+v", state.Outcome, want)
	}
	fixture.assertRated(t, []float64{1500, 1500}, []models.RatingResult{models.RatingDraw, models.RatingDraw}, []int{40, 40})
}

// A three-team game of new competitors: the winner gains 20 and each other
// team loses 10.
func TestARatedGameOfThreeTeamsScoresTheWinnerAgainstEachOtherTeam(t *testing.T) {
	fixture := newRatedFixture(t, capSettings(1, 1, 2, 3), true, 3)
	state := fixture.playFirstTurn(t)
	want := fixture.ratingChanges(1520, 1490, 1490)
	if state.Outcome.WinnerTeamColor == nil || *state.Outcome.WinnerTeamColor != 1 || !reflect.DeepEqual(state.Outcome.Ratings, want) {
		t.Fatalf("outcome = %+v, want Blue to win with ratings %+v", state.Outcome, want)
	}
	fixture.assertRated(t, []float64{1520, 1490, 1490}, []models.RatingResult{models.RatingWin, models.RatingLoss, models.RatingLoss}, []int{40, 40, 40})
}

// A competitor's 31st rated game uses K 20.
func TestACompetitorsThirtyFirstRatedGameUsesK20(t *testing.T) {
	fixture := newRatedFixture(t, capSettings(1, 1, 2), true, 2)
	if err := fixture.database.Model(&models.Competitor{ID: fixture.competitors[0].ID}).Select("Games", "Wins").Updates(&models.Competitor{Games: 30, Wins: 30}).Error; err != nil {
		t.Fatal(err)
	}
	fixture.competitors[0].Games = 30
	state := fixture.playFirstTurn(t)
	if got := state.Outcome.Ratings; len(got) != 2 || got[0].After != 1510 || got[1].After != 1480 {
		t.Fatalf("ratings = %+v, want Kraken at 1510 with K 20 and Leviathan at 1480 with K 40", got)
	}
	kraken := fixture.competitor(t, fixture.competitors[0].ID)
	if kraken.Rating != 1510 || kraken.Games != 31 || kraken.Wins != 31 {
		t.Fatalf("Kraken after its 31st game = %+v", kraken)
	}
	events := fixture.ratingEvents(t)
	if events[fixture.competitors[0].ID].K != 20 || events[fixture.competitors[1].ID].K != 40 {
		t.Fatalf("history = %+v", events)
	}
}

// A game that ends by the turn limit is rated like any other.
func TestARatedGameThatEndsByTheTurnLimitIsRated(t *testing.T) {
	fixture := newRatedFixture(t, mapSettings("30", 1, 2), true, 2)
	alex := fixture.players[0][0]
	fixture.complete(t, alex)
	*fixture.clock = fixture.deadline(t).Add(time.Second)
	state := fixture.applyDeadline(t, alex)
	want := fixture.ratingChanges(1520, 1480)
	if state.Outcome == nil || state.Outcome.Reason != models.GameOutcomeTurnTimeout || !reflect.DeepEqual(state.Outcome.Ratings, want) {
		t.Fatalf("outcome = %+v, want Sam's team to lose by the turn limit with ratings %+v", state.Outcome, want)
	}
	fixture.assertRated(t, []float64{1520, 1480}, []models.RatingResult{models.RatingWin, models.RatingLoss}, []int{40, 40})
}

// A three-team game ends at the turn limit: Red missed it, and Blue and
// Green tie on castles. Red loses to each survivor, and they draw each
// other. Each pair weighs 40 / 2 = 20: Red loses 20 × 0.5 to Blue and to
// Green, 1480, and Blue and Green each gain 20 × 0.5 from Red and nothing
// from each other, 1510.
func TestARatedDrawLosesATeamThatTimedOutToEachSurvivor(t *testing.T) {
	fixture := newRatedFixture(t, mapSettings("30", 1, 2, 3), true, 3)
	alex, kim := fixture.players[0][0], fixture.players[2][0]
	fixture.complete(t, alex)
	fixture.complete(t, kim)
	*fixture.clock = fixture.deadline(t).Add(time.Second)
	state := fixture.applyDeadline(t, alex)
	want := fixture.ratingChanges(1510, 1480, 1510)
	if state.Outcome == nil || state.Outcome.Reason != models.GameOutcomeTurnTimeout || state.Outcome.WinnerTeamColor != nil || !reflect.DeepEqual(state.Outcome.Ratings, want) {
		t.Fatalf("outcome = %+v, want Red to lose by the turn limit and Blue and Green to draw, with ratings %+v", state.Outcome, want)
	}
	if saved := fixture.savedOutcome(t); !reflect.DeepEqual(saved.Survivors, []uint{1, 3}) {
		t.Fatalf("survivors = %v, want Blue and Green", saved.Survivors)
	}
	fixture.assertRated(t, []float64{1510, 1480, 1510}, []models.RatingResult{models.RatingDraw, models.RatingLoss, models.RatingDraw}, []int{40, 40, 40})
}

// A three-team game where every team missed the turn limit has no survivor:
// every pair draws, so new competitors keep 1500 and each counts a draw.
func TestARatedGameWithoutASurvivorIsADrawForEveryTeam(t *testing.T) {
	fixture := newRatedFixture(t, mapSettings("30", 1, 2, 3), true, 3)
	*fixture.clock = fixture.deadline(t).Add(time.Second)
	state := fixture.applyDeadline(t, fixture.players[0][0])
	want := fixture.ratingChanges(1500, 1500, 1500)
	if state.Outcome == nil || state.Outcome.Reason != models.GameOutcomeTurnTimeout || state.Outcome.WinnerTeamColor != nil || !reflect.DeepEqual(state.Outcome.Ratings, want) {
		t.Fatalf("outcome = %+v, want a draw with ratings %+v", state.Outcome, want)
	}
	if saved := fixture.savedOutcome(t); len(saved.TimedOut) != 3 || len(saved.Survivors) != 0 {
		t.Fatalf("outcome = %+v, want every team to time out", saved)
	}
	fixture.assertRated(t, []float64{1500, 1500, 1500}, []models.RatingResult{models.RatingDraw, models.RatingDraw, models.RatingDraw}, []int{40, 40, 40})
}

// A team eliminated before a draw at the generation cap loses to each
// survivor too: Red's castle is neutral when the last turn plays, and Blue
// and Green tie on one castle each, 1510, 1480 and 1510 as above.
func TestARatedDrawAtTheCapLosesAnEliminatedTeamToEachSurvivor(t *testing.T) {
	fixture := newRatedFixture(t, capSettings(1, 2, 3), true, 3)
	generation, err := dal.GetLatestGenerationForGame(fixture.database, fixture.states[fixture.players[0][0].PlayerID].GameID)
	if err != nil {
		t.Fatal(err)
	}
	for _, castle := range generation.Map.Castles {
		if castle.TeamColor == red {
			if err := fixture.database.Model(&models.Castle{Model: gorm.Model{ID: castle.ID}}).Select("TeamColor").Updates(&models.Castle{CastleFields: models.CastleFields{TeamColor: models.NeutralTeamColor}}).Error; err != nil {
				t.Fatal(err)
			}
		}
	}
	state := fixture.playFirstTurn(t)
	want := fixture.ratingChanges(1510, 1480, 1510)
	if state.Outcome.Reason != models.GameOutcomeGenerationCap || state.Outcome.WinnerTeamColor != nil || !reflect.DeepEqual(state.Outcome.Ratings, want) {
		t.Fatalf("outcome = %+v, want Blue and Green to draw, with ratings %+v", state.Outcome, want)
	}
	if saved := fixture.savedOutcome(t); !reflect.DeepEqual(saved.Survivors, []uint{1, 3}) {
		t.Fatalf("survivors = %v, want Blue and Green", saved.Survivors)
	}
	fixture.assertRated(t, []float64{1510, 1480, 1510}, []models.RatingResult{models.RatingDraw, models.RatingLoss, models.RatingDraw}, []int{40, 40, 40})
}

// An unrated game writes no history row, changes no rating, and its
// outcome has no ratings.
func TestAnUnratedGameChangesNoRating(t *testing.T) {
	fixture := newRatedFixture(t, capSettings(1, 1, 2), false, 2)
	state := fixture.playFirstTurn(t)
	if state.Outcome.Ratings != nil {
		t.Fatalf("outcome = %+v, want no ratings", state.Outcome)
	}
	encoded, err := json.Marshal(state.Outcome)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if err := json.Unmarshal(encoded, &fields); err != nil {
		t.Fatal(err)
	}
	if _, found := fields["ratings"]; found {
		t.Fatalf("outcome JSON = %s, want no ratings", encoded)
	}
	var events int64
	if err := fixture.database.Model(&models.RatingEvent{}).Count(&events).Error; err != nil || events != 0 {
		t.Fatalf("%d rating history rows, %v", events, err)
	}
	for _, view := range fixture.competitors {
		if competitor := fixture.competitor(t, view.ID); competitor.Rating != 1500 || competitor.Games != 0 || competitor.LastPlayedAt != nil {
			t.Fatalf("competitor after an unrated game = %+v", competitor)
		}
	}
}

// Storing a second outcome for the same game can't change a rating: the
// unique index on the game and the competitor refuses the second rating, and
// its transaction changes nothing.
func TestARatedGameIsRatedOnce(t *testing.T) {
	fixture := newRatedFixture(t, mapSettings("null", 1, 2), true, 2)
	game, err := dal.FindLobbyGame(fixture.database, fixture.lobbyID)
	if err != nil {
		t.Fatal(err)
	}
	players, err := dal.LoadFrozenPlayers(fixture.database, game.ID)
	if err != nil {
		t.Fatal(err)
	}
	rate := func() (models.GameOutcome, error) {
		outcome := models.GameOutcome{Reason: models.GameOutcomeLastTeamStanding, WinnerTeamColor: colour(1)}
		err := fixture.database.Transaction(func(transaction *gorm.DB) error {
			return services.RateGame(transaction, game, players, &outcome)
		})
		return outcome, err
	}
	first, err := rate()
	if err != nil {
		t.Fatal(err)
	}
	if want := fixture.ratingChanges(1520, 1480); !reflect.DeepEqual(first.Ratings, want) {
		t.Fatalf("first rating = %+v, want %+v", first.Ratings, want)
	}
	// The unique (game, competitor) history row refuses a second rating.
	if _, err := rate(); !errors.Is(err, gorm.ErrDuplicatedKey) && !strings.Contains(fmt.Sprint(err), "23505") {
		t.Fatalf("rating the game again = %v, want a unique violation", err)
	}
	fixture.assertRated(t, []float64{1520, 1480}, []models.RatingResult{models.RatingWin, models.RatingLoss}, []int{40, 40})
}

// After a retention pass deletes a rated game, its history rows and its
// competitors' ratings are unchanged: they outlive the game.
func TestRatingsOutliveTheGame(t *testing.T) {
	fixture := newRatedFixture(t, capSettings(1, 1, 2), true, 2)
	gameID := fixture.playFirstTurn(t).GameID
	before := fixture.ratingEvents(t)
	runner, err := retention.New(retention.DatabaseStore{Database: fixture.database}, retention.DefaultPolicy(), time.Now, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	later := time.Now().Add(30 * 24 * time.Hour)
	for range 3 {
		if _, err := runner.Pass(context.Background(), later); err != nil {
			t.Fatal(err)
		}
	}
	var games int64
	if err := fixture.database.Unscoped().Model(&models.Game{}).Where(&models.Game{Model: gorm.Model{ID: gameID}}).Count(&games).Error; err != nil || games != 0 {
		t.Fatalf("%d games after retention, %v: want the game deleted", games, err)
	}
	var after []models.RatingEvent
	if err := fixture.database.Where(&models.RatingEvent{GameID: gameID}).Find(&after).Error; err != nil {
		t.Fatal(err)
	}
	if len(after) != len(before) {
		t.Fatalf("%d history rows after retention, want %d", len(after), len(before))
	}
	for _, event := range after {
		if kept := before[event.CompetitorID]; !reflect.DeepEqual(event.Opponents, kept.Opponents) || event.RatingAfter != kept.RatingAfter || event.Result != kept.Result {
			t.Fatalf("history row after retention = %+v, before %+v", event, kept)
		}
	}
	fixture.assertRated(t, []float64{1520, 1480}, []models.RatingResult{models.RatingWin, models.RatingLoss}, []int{40, 40})
}
