package services_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/INTODAN/mpl/domain"
	"islandBattles/backend/dal"
	"islandBattles/backend/internal/testutils"
	"islandBattles/backend/models"
	"islandBattles/backend/services"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// awayAccounts registers and lists bot names for the away bots' tests.
type awayAccounts struct {
	database *gorm.DB
	accounts services.Accounts
}

func newAwayAccounts(t *testing.T) awayAccounts {
	t.Helper()
	database := testutils.InitTestDB(t)
	return awayAccounts{database: database, accounts: services.Accounts{Database: database, ServerBots: services.ServerBots{Resolver: testutils.BotHosts}}}
}

// person is a new signed-in person.
func (fixture awayAccounts) person(name string) domain.Principal {
	return domain.Principal{PlayerID: uuid.NewString(), DisplayName: name, AccountID: uuid.NewString()}
}

// listedBot registers a bot name of owner, with one rated game, which lists
// it on the leaderboard.
func (fixture awayAccounts) listedBot(t *testing.T, owner domain.Principal, name string) models.CompetitorView {
	t.Helper()
	competitor, err := fixture.accounts.RegisterBotName(context.Background(), owner, name, "https://bot.example.com/"+strings.ToLower(name))
	if err != nil {
		t.Fatal(err)
	}
	if err := fixture.database.Model(&models.Competitor{}).Where(&models.Competitor{ID: competitor.ID}).
		Updates(map[string]any{"games": 1, "wins": 1, "rating": 1520, "last_played_at": time.Now()}).Error; err != nil {
		t.Fatal(err)
	}
	competitor.Games, competitor.Rating = 1, 1520
	return competitor
}

// awayTeam is one team of an away bots' test game: a person with their
// member settings, or a bot seat with its seat settings.
type awayTeam struct {
	person   *domain.Principal
	settings string
}

func byHand(person domain.Principal) awayTeam {
	return awayTeam{person: &person, settings: `{"bot":{"kind":"manual"}}`}
}

func choosing(person domain.Principal, competitorID string) awayTeam {
	return awayTeam{person: &person, settings: `{"bot":{"kind":"hosted","competitor_id":"` + competitorID + `"}}`}
}

func named(person domain.Principal, name string) awayTeam {
	return awayTeam{person: &person, settings: `{"bot":{"kind":"hosted","name":"` + name + `"}}`}
}

func seated(competitorID string) awayTeam {
	return awayTeam{settings: `{"bot_competitor_id":"` + competitorID + `"}`}
}

// awayGame is a started game of awayTeams: its fixture, with each team's
// player, a seat's as a principal of its player ID.
type awayGame struct {
	deadlineFixture
	gameID uint
}

// start starts a rated game of teams in a new lobby, on settings' map
// (mapSettings), in colour order, and returns it, or the start's error.
func (fixture awayAccounts) start(t *testing.T, settings string, teams ...awayTeam) (awayGame, error) {
	t.Helper()
	fields := map[string]any{}
	if err := json.Unmarshal([]byte(settings), &fields); err != nil {
		t.Fatal(err)
	}
	fields["rated"] = true
	combined, err := json.Marshal(fields)
	if err != nil {
		t.Fatal(err)
	}
	game := awayGame{deadlineFixture: deadlineFixture{database: fixture.database, publisher: &testutils.RecordedPublisher{}, lobbyID: uuid.NewString(), states: map[string]models.GenerationState{}}}
	snapshot := domain.Snapshot{Lobby: domain.Lobby{ID: game.lobbyID, Round: 1, GameKey: "island-battles", Phase: domain.PhaseStarted, GameSettings: combined}}
	for index, team := range teams {
		lobbyTeam := domain.Team{ID: uuid.NewString(), Order: index + 1, Name: deadlineTeamNames[index]}
		snapshot.Teams = append(snapshot.Teams, lobbyTeam)
		member := domain.Member{ID: uuid.NewString(), PlayerID: uuid.NewString(), TeamID: &lobbyTeam.ID, Active: true}
		if team.person != nil {
			accountID := team.person.AccountID
			member.PlayerID, member.DisplayName, member.AccountID, member.MemberSettings = team.person.PlayerID, team.person.DisplayName, &accountID, json.RawMessage(team.settings)
			game.players = append(game.players, []domain.Principal{*team.person})
		} else {
			member.Kind, member.DisplayName, member.SeatSettings = domain.MemberSeat, "Seat", json.RawMessage(team.settings)
			game.players = append(game.players, []domain.Principal{{PlayerID: member.PlayerID, DisplayName: "Seat"}})
		}
		snapshot.Members = append(snapshot.Members, member)
		snapshot.StartRoster = append(snapshot.StartRoster, domain.StartRosterEntry{PlayerID: member.PlayerID, TeamID: lobbyTeam.ID})
	}
	binding, err := services.RunLobbyGame(context.Background(), fixture.database, game.publisher, nil, snapshot)
	if err != nil {
		return awayGame{}, err
	}
	game.gameID = binding.GameID
	clock := time.Now()
	game.clock = &clock
	game.service = &services.GenerationService{Database: fixture.database, Publisher: game.publisher, Now: func() time.Time { return *game.clock }}
	for _, team := range game.players {
		game.states[team[0].PlayerID] = game.current(t, team[0])
	}
	return game, nil
}

// mustStart starts a rated game as start does, and fails the test if the
// start is refused.
func (fixture awayAccounts) mustStart(t *testing.T, settings string, teams ...awayTeam) awayGame {
	t.Helper()
	game, err := fixture.start(t, settings, teams...)
	if err != nil {
		t.Fatal(err)
	}
	return game
}

// frozen reads the game's frozen players, by player ID.
func (game awayGame) frozen(t *testing.T) map[string]models.FrozenGamePlayer {
	t.Helper()
	players, err := dal.LoadFrozenPlayers(game.database, game.gameID)
	if err != nil {
		t.Fatal(err)
	}
	byPlayer := map[string]models.FrozenGamePlayer{}
	for _, player := range players {
		byPlayer[player.PlayerID] = player
	}
	return byPlayer
}

// playToTheCap has every player complete the first turn, which ends the
// game at the generation cap (capSettings).
func (game awayGame) playToTheCap(t *testing.T) models.GenerationState {
	t.Helper()
	if err := game.database.Model(&models.Game{Model: gorm.Model{ID: game.gameID}}).Select("MaxGenerations").Updates(&models.Game{MaxGenerations: 2}).Error; err != nil {
		t.Fatal(err)
	}
	var state models.GenerationState
	for _, team := range game.players {
		state = game.complete(t, team[0])
	}
	if state.Outcome == nil {
		t.Fatalf("the first turn didn't end the game: %+v", state)
	}
	return state
}

// expectRefusal checks that err is the game_requirement problemType, with
// detail when it isn't empty.
func expectRefusal(t *testing.T, err error, problemType, detail string) {
	t.Helper()
	var refusal *domain.Error
	if !errors.As(err, &refusal) || refusal.Code != domain.CodeGameRequirement || refusal.Type != problemType || (detail != "" && refusal.Message != detail) {
		t.Fatalf("start = %v, want %s: %q", err, problemType, detail)
	}
}

// A signed-in host who owns neither bot runs a rated game between two
// listed bots: Kraken for their own team and Leviathan in a seat. Both play
// away, at their names' URLs, and the game plays to its end without their
// owners: Kraken's and Leviathan's ratings change, and the host's hand play
// isn't in the game. Their history marks the results as away.
func TestAHostRunsARatedGameBetweenTwoListedBotsThatArentTheirs(t *testing.T) {
	fixture := newAwayAccounts(t)
	dana, erin, fay := fixture.person("Dana"), fixture.person("Erin"), fixture.person("Fay")
	kraken, leviathan := fixture.listedBot(t, dana, "Kraken"), fixture.listedBot(t, erin, "Leviathan")
	game := fixture.mustStart(t, capSettings(1, 1, 2), choosing(fay, kraken.ID), seated(leviathan.ID))

	frozen := game.frozen(t)
	for player, want := range map[string]models.CompetitorView{fay.PlayerID: kraken, game.players[1][0].PlayerID: leviathan} {
		got := frozen[player]
		if got.CompetitorID == nil || *got.CompetitorID != want.ID || !got.Away || got.BotKind != models.HostedBot || got.BotURL == nil || *got.BotURL != *want.URL || got.BotName == nil || *got.BotName != want.Name {
			t.Fatalf("frozen player = %+v, want %s away at %s", got, want.Name, *want.URL)
		}
	}
	state := game.playToTheCap(t)
	if len(state.Outcome.Ratings) != 2 || state.Outcome.Ratings[0].CompetitorID != kraken.ID || state.Outcome.Ratings[1].CompetitorID != leviathan.ID ||
		state.Outcome.Ratings[0].After <= kraken.Rating || state.Outcome.Ratings[1].After >= leviathan.Rating {
		t.Fatalf("ratings = %+v, want Kraken to beat Leviathan", state.Outcome.Ratings)
	}
	if hand, err := dal.FindManualCompetitor(fixture.database, fay.AccountID); err != nil || hand != nil {
		t.Fatalf("Fay's hand play = %+v, %v; want none", hand, err)
	}
	page, err := services.Leaderboard{Database: fixture.database}.Competitor(context.Background(), kraken.ID)
	if err != nil || len(page.History) != 1 || !page.History[0].Away {
		t.Fatalf("Kraken's page = %+v, %v; want one away result", page, err)
	}
}

// A listed bot in a seat against the host's hand play rates both: the hand
// play at home, the bot away.
func TestAListedBotSeatAgainstHandPlayRatesBoth(t *testing.T) {
	fixture := newAwayAccounts(t)
	dana, fay := fixture.person("Dana"), fixture.person("Fay")
	kraken := fixture.listedBot(t, dana, "Kraken")
	game := fixture.mustStart(t, capSettings(1, 1, 2), byHand(fay), seated(kraken.ID))
	hand, err := dal.FindManualCompetitor(fixture.database, fay.AccountID)
	if err != nil || hand == nil {
		t.Fatalf("Fay's hand play = %+v, %v", hand, err)
	}
	frozen := game.frozen(t)
	if seat := frozen[game.players[1][0].PlayerID]; !seat.Away || *seat.CompetitorID != kraken.ID {
		t.Fatalf("Kraken's seat = %+v", seat)
	}
	if person := frozen[fay.PlayerID]; person.Away || *person.CompetitorID != hand.ID {
		t.Fatalf("Fay = %+v", person)
	}
	state := game.playToTheCap(t)
	if len(state.Outcome.Ratings) != 2 || state.Outcome.Ratings[0].CompetitorID != hand.ID || state.Outcome.Ratings[1].CompetitorID != kraken.ID {
		t.Fatalf("ratings = %+v", state.Outcome.Ratings)
	}
	var events []models.RatingEvent
	if err := fixture.database.Where(&models.RatingEvent{GameID: game.gameID}).Find(&events).Error; err != nil || len(events) != 2 {
		t.Fatalf("history = %+v, %v", events, err)
	}
	for _, event := range events {
		if event.Away != (event.CompetitorID == kraken.ID) {
			t.Fatalf("history row = %+v", event)
		}
	}
}

// Two competitors, one of them away, play one rated game a day with each of
// them on the lower team colour: a second the same day only with their teams
// swapped, and no third. A new UTC day allows them again.
func TestTwoCompetitorsPlayOneRatedGameEachWayADay(t *testing.T) {
	fixture := newAwayAccounts(t)
	dana, fay := fixture.person("Dana"), fixture.person("Fay")
	kraken := fixture.listedBot(t, dana, "Kraken")
	settings := mapSettings("30", 1, 2)
	first := fixture.mustStart(t, settings, byHand(fay), seated(kraken.ID))
	_, err := fixture.start(t, settings, byHand(fay), seated(kraken.ID))
	expectRefusal(t, err, services.RatedPairLimitProblemType, "Fay and Kraken have played their rated game with Fay on Blue today. Swap their teams, or play again tomorrow (UTC).")
	second := fixture.mustStart(t, settings, seated(kraken.ID), byHand(fay))
	for detail, teams := range map[string][]awayTeam{"Fay and Kraken": {byHand(fay), seated(kraken.ID)}, "Kraken and Fay": {seated(kraken.ID), byHand(fay)}} {
		_, err := fixture.start(t, settings, teams...)
		expectRefusal(t, err, services.RatedPairLimitProblemType, detail+" have played both their rated games today. Play again tomorrow (UTC).")
	}
	// Either game's day ended: yesterday's games count no more.
	for _, game := range []awayGame{first, second} {
		if err := fixture.database.Model(&models.FrozenGamePlayer{}).Where(&models.FrozenGamePlayer{GameID: game.gameID}).
			Update("created_at", time.Now().UTC().Add(-24*time.Hour)).Error; err != nil {
			t.Fatal(err)
		}
	}
	fixture.mustStart(t, settings, byHand(fay), seated(kraken.ID))
}

// An away bot plays at most ten rated games away a UTC day, against all
// opponents together. Its owner's own games with it are neither counted nor
// refused, and a member of another account who chooses it plays it away.
func TestABotPlaysTenRatedGamesAwayADay(t *testing.T) {
	fixture := newAwayAccounts(t)
	dana := fixture.person("Dana")
	kraken := fixture.listedBot(t, dana, "Kraken")
	settings := mapSettings("30", 1, 2)
	house := awayTeam{settings: `{"bot_level":2}`}
	for game := range services.MaxAwayGamesPerDay {
		opponent := fixture.person("Opponent")
		if game%2 == 0 {
			fixture.mustStart(t, settings, byHand(opponent), seated(kraken.ID))
		} else {
			fixture.mustStart(t, settings, choosing(opponent, kraken.ID), byHand(fixture.person("Another")))
		}
		// The owner's own games, as a name or by its ID, count nothing.
		fixture.mustStart(t, settings, named(dana, "Kraken"), house)
	}
	own := fixture.mustStart(t, settings, choosing(dana, kraken.ID), house)
	if player := own.frozen(t)[dana.PlayerID]; player.Away || *player.CompetitorID != kraken.ID {
		t.Fatalf("Dana's own Kraken = %+v, want it at home", player)
	}
	_, err := fixture.start(t, settings, byHand(fixture.person("Eleventh")), seated(kraken.ID))
	expectRefusal(t, err, services.RatedBotDayLimitProblemType, "Kraken has played its 10 rated games away from its owner today. Play it again tomorrow (UTC).")
}

// Two starts at once of the same pair, on the same teams, can't both pass
// the pair's cap: the second waits for the first's lock on the away bot, and
// then counts its game.
func TestTwoStartsAtOnceCantBothPassACap(t *testing.T) {
	fixture := newAwayAccounts(t)
	dana, fay := fixture.person("Dana"), fixture.person("Fay")
	kraken := fixture.listedBot(t, dana, "Kraken")
	settings := mapSettings("30", 1, 2)
	const starts = 4
	errs := make(chan error, starts)
	begin := make(chan struct{})
	var group sync.WaitGroup
	for range starts {
		group.Go(func() {
			<-begin
			_, err := fixture.start(t, settings, byHand(fay), seated(kraken.ID))
			errs <- err
		})
	}
	close(begin)
	group.Wait()
	close(errs)
	started := 0
	for err := range errs {
		if err == nil {
			started++
			continue
		}
		expectRefusal(t, err, services.RatedPairLimitProblemType, "")
	}
	if started != 1 {
		t.Fatalf("%d starts passed the cap, want 1", started)
	}
}

// An away bot that misses a turn loses, and is paused: it plays away in no
// rated game until its owner resumes it, and the account page shows why.
// Its owner's own games with it go on.
func TestAnAwayBotThatMissesATurnIsPaused(t *testing.T) {
	fixture := newAwayAccounts(t)
	dana, erin, fay := fixture.person("Dana"), fixture.person("Erin"), fixture.person("Fay")
	kraken, leviathan := fixture.listedBot(t, dana, "Kraken"), fixture.listedBot(t, erin, "Leviathan")
	settings := mapSettings("30", 1, 2)
	game := fixture.mustStart(t, settings, choosing(fay, kraken.ID), seated(leviathan.ID))
	game.complete(t, fay)
	*game.clock = game.deadline(t).Add(time.Second)
	state := game.applyDeadline(t, fay)
	if state.Outcome == nil || state.Outcome.Reason != models.GameOutcomeTurnTimeout || state.Outcome.WinnerTeamColor == nil || *state.Outcome.WinnerTeamColor != 1 {
		t.Fatalf("outcome = %+v, want Leviathan to lose by the turn limit", state.Outcome)
	}
	account, err := fixture.accounts.Read(context.Background(), erin)
	if err != nil || len(account.BotNames) != 1 {
		t.Fatalf("Erin's account = %+v, %v", account, err)
	}
	paused := account.BotNames[0].AwayPaused
	wantReason := "Missed turn 1 of a rated game on " + time.Now().UTC().Format("2 January")
	if paused == nil || paused.Reason != wantReason {
		t.Fatalf("Leviathan's pause = %+v, want %q", paused, wantReason)
	}
	if kraken, err := fixture.accounts.Read(context.Background(), dana); err != nil || kraken.BotNames[0].AwayPaused != nil {
		t.Fatalf("Dana's account = %+v, %v; want Kraken unpaused", kraken, err)
	}

	_, err = fixture.start(t, settings, byHand(fay), seated(leviathan.ID))
	expectRefusal(t, err, services.RatedBotPausedProblemType, "Leviathan is paused after it missed a turn, until its owner resumes it. Remove Seat.")
	_, err = fixture.start(t, settings, choosing(fay, leviathan.ID), seated(kraken.ID))
	expectRefusal(t, err, services.RatedBotPausedProblemType, "Leviathan is paused after it missed a turn, until its owner resumes it: Fay must choose another bot.")
	fixture.mustStart(t, settings, named(erin, "Leviathan"), seated(kraken.ID))

	if _, err := fixture.accounts.ResumeAway(context.Background(), dana, leviathan.ID); err == nil {
		t.Fatal("Dana resumed Erin's bot")
	}
	resumed, err := fixture.accounts.ResumeAway(context.Background(), erin, leviathan.ID)
	if err != nil || resumed.AwayPaused != nil {
		t.Fatalf("resumed Leviathan = %+v, %v", resumed, err)
	}
	fixture.mustStart(t, settings, byHand(fay), seated(leviathan.ID))
}

// No two competitors of a rated game belong to one account, whoever chose
// them; an unrated bot plays away, but not one of a hidden account.
func TestARatedStartRefusesTwoCompetitorsOfOneAccountAndUnregisteredBots(t *testing.T) {
	fixture := newAwayAccounts(t)
	dana, fay := fixture.person("Dana"), fixture.person("Fay")
	kraken, leviathan := fixture.listedBot(t, dana, "Kraken"), fixture.listedBot(t, dana, "Leviathan")
	settings := mapSettings("30", 1, 2)
	for _, refused := range []struct {
		teams  []awayTeam
		detail string
	}{
		{[]awayTeam{choosing(fay, kraken.ID), seated(leviathan.ID)}, "Kraken and Leviathan are both Dana's bots. A rated game needs a different account on each team."},
		{[]awayTeam{byHand(dana), seated(kraken.ID)}, "Kraken is Dana's bot, and Dana plays by hand. A rated game needs a different account on each team."},
		{[]awayTeam{named(dana, "Leviathan"), seated(kraken.ID)}, "Leviathan and Kraken are both Dana's bots. A rated game needs a different account on each team."},
	} {
		_, err := fixture.start(t, settings, refused.teams...)
		expectRefusal(t, err, services.RatedSameAccountProblemType, refused.detail)
	}
	_, err := fixture.start(t, mapSettings("30", 1, 2, 3), seated(kraken.ID), seated(kraken.ID), byHand(fay))
	expectRefusal(t, err, services.RatedSameAccountProblemType, "Kraken can play for one team only. A rated game needs a different account on each team.")

	erin := fixture.person("Erin")
	hydra, err := fixture.accounts.RegisterBotName(context.Background(), erin, "Hydra", "https://bot.example.com/hydra")
	if err != nil {
		t.Fatal(err)
	}
	// Hydra has played no rated game: it isn't on the leaderboard, yet anyone may choose it.
	fixture.mustStart(t, settings, byHand(fay), seated(hydra.ID))
	if _, err := dal.SetAccountHidden(fixture.database, erin.AccountID, true); err != nil {
		t.Fatal(err)
	}
	_, err = fixture.start(t, settings, seated(hydra.ID), byHand(fay))
	expectRefusal(t, err, services.RatedBotNotListedProblemType, "Hydra is no longer a registered bot with a URL. Remove Seat.")
	_, err = fixture.start(t, settings, choosing(fay, hydra.ID), seated(kraken.ID))
	expectRefusal(t, err, services.RatedBotNotListedProblemType, "Hydra, which Fay chose, is no longer a registered bot with a URL: they must choose another bot.")
	// Its owner may choose their own unrated bot by its ID, as if they named it.
	gorgon, err := fixture.accounts.RegisterBotName(context.Background(), dana, "Gorgon", "https://bot.example.com/gorgon")
	if err != nil {
		t.Fatal(err)
	}
	fixture.mustStart(t, settings, choosing(dana, gorgon.ID), awayTeam{settings: `{"bot_level":2}`})
}

// In a real-time game, which plays on without a bot that doesn't answer, an
// away bot that the silence rule takes out has missed its turns too: it
// loses, and is paused.
func TestAnAwayBotThatLeavesARealTimeGameIsPaused(t *testing.T) {
	fixture := newAwayAccounts(t)
	dana, fay := fixture.person("Dana"), fixture.person("Fay")
	kraken := fixture.listedBot(t, dana, "Kraken")
	game := fixture.mustStart(t, mapSettings("10", 1, 2), byHand(fay), seated(kraken.ID))
	state := game.current(t, fay)
	for turns := 0; state.Outcome == nil; turns++ {
		if turns > 1000 || state.TurnDeadlineAt == nil {
			t.Fatalf("the game didn't end: %+v", state)
		}
		*game.clock = state.TurnDeadlineAt.Add(time.Millisecond)
		state = game.applyDeadline(t, fay)
	}
	if len(state.Outcome.Departures) != 1 || state.Outcome.Departures[0].Kind != models.DepartureLeft || state.Outcome.WinnerTeamColor == nil || *state.Outcome.WinnerTeamColor != 1 {
		t.Fatalf("outcome = %+v, want Kraken to leave", state.Outcome)
	}
	account, err := fixture.accounts.Read(context.Background(), dana)
	if err != nil || len(account.BotNames) != 1 || account.BotNames[0].AwayPaused == nil || !strings.HasPrefix(account.BotNames[0].AwayPaused.Reason, "Missed turn ") {
		t.Fatalf("Dana's account = %+v, %v; want Kraken paused", account, err)
	}
}

// A rated start whose roster has a member without a bot is refused with
// bot-not-chosen, never half rated.
func TestARatedStartRefusesAMemberWithoutABot(t *testing.T) {
	fixture := newAwayAccounts(t)
	dana, fay := fixture.person("Dana"), fixture.person("Fay")
	kraken := fixture.listedBot(t, dana, "Kraken")
	_, err := fixture.start(t, mapSettings("30", 1, 2), awayTeam{person: &fay, settings: `{}`}, seated(kraken.ID))
	expectRefusal(t, err, services.BotNotChosenProblemType, "")
}
