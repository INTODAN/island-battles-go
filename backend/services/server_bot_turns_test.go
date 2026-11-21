package services_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/INTODAN/mpl/domain"
	"net/url"

	"islandBattles/backend/dal"
	"islandBattles/backend/demotoken"
	"islandBattles/backend/internal/testutils"
	"islandBattles/backend/models"
	"islandBattles/backend/services"
	"islandBattles/backend/submittoken"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// invocation is one event a fake runner was given, with what the database
// held when it was given it.
type invocation struct {
	runner models.BotRunnerName
	event  models.BotRunnerEvent
	// committed is whether the event's generation was visible outside the
	// transaction that created it.
	committed bool
}

// fakeRunners records the events the server sends, as a fake Lambda would,
// and refuses those of the players in failing.
type fakeRunners struct {
	database *gorm.DB
	tokens   *submittoken.Signer
	demos    *demotoken.Signer
	mutex    sync.Mutex
	events   []invocation
	failing  map[string]bool
}

func (runners *fakeRunners) Invoke(ctx context.Context, runner models.BotRunnerName, event models.BotRunnerEvent) error {
	if event.GameEnded != nil {
		return runners.invokeGameEnded(ctx, runner, event)
	}
	claims, err := runners.tokens.Verify(event.Delivery.Submit.Token, time.Now())
	if err != nil {
		return err
	}
	var found []models.Generation
	committed := runners.database.WithContext(ctx).Where(&models.Generation{GameID: claims.GameID}).Where("id = ?", claims.GenerationID).Limit(1).Find(&found).Error == nil && len(found) == 1
	runners.mutex.Lock()
	defer runners.mutex.Unlock()
	runners.events = append(runners.events, invocation{runner: runner, event: event, committed: committed})
	if runners.failing[claims.PlayerID] {
		return errors.New("TooManyRequestsException: Rate Exceeded")
	}
	return nil
}

// invokeGameEnded records the end of a game, whose event is committed once
// the game's outcome is visible outside the transaction that stored it.
func (runners *fakeRunners) invokeGameEnded(ctx context.Context, runner models.BotRunnerName, event models.BotRunnerEvent) error {
	claims, err := demoClaims(runners.demos, event.GameEnded.Demo)
	if err != nil {
		return err
	}
	game, err := dal.FindGame(runners.database.WithContext(ctx), claims.GameID)
	committed := err == nil && game.Outcome != nil
	runners.mutex.Lock()
	defer runners.mutex.Unlock()
	runners.events = append(runners.events, invocation{runner: runner, event: event, committed: committed})
	if runners.failing[claims.PlayerID] {
		return errors.New("TooManyRequestsException: Rate Exceeded")
	}
	return nil
}

// demoClaims verifies the demo token of a demo link.
func demoClaims(demos *demotoken.Signer, link models.DemoLink) (demotoken.Claims, error) {
	parsed, err := url.Parse(link.URL)
	if err != nil {
		return demotoken.Claims{}, err
	}
	return demos.Verify(parsed.Query().Get("token"), time.Now())
}

// take returns the events sent since the last take.
func (runners *fakeRunners) take() []invocation {
	runners.mutex.Lock()
	defer runners.mutex.Unlock()
	taken := runners.events
	runners.events = nil
	return taken
}

// fakeLinks presigns a fake GET of each key.
type fakeLinks struct{}

func (fakeLinks) PresignDownload(_ context.Context, key string, expires time.Duration) (string, error) {
	return fmt.Sprintf("https://packages.example/%s?expires=%s", key, expires), nil
}

const (
	serverBotOrigin    = "https://island-battles.example.org"
	serverBotSubmitURL = serverBotOrigin + "/generation/commands"
)

// serverBotGame is a started game with bots that the server plays.
type serverBotGame struct {
	database *gorm.DB
	runners  *fakeRunners
	tokens   *submittoken.Signer
	demos    *demotoken.Signer
	service  *services.GenerationService
	lobbyID  string
	gameID   uint
	players  []domain.Principal
}

// startServerBotGame starts a game of one player per bot, each on a team of
// its own, after storing the lobby's ready packages. A zero ServerBot leaves
// the player to their localhost bot.
func startServerBotGame(t *testing.T, packages []models.BotPackage, bots []services.ServerBot, failing ...int) serverBotGame {
	t.Helper()
	database := testutils.InitTestDB(t)
	tokens, err := submittoken.New([]byte("server-bot-test-secret"))
	if err != nil {
		t.Fatal(err)
	}
	game := serverBotGame{database: database, tokens: tokens, lobbyID: uuid.NewString()}
	demos, err := demotoken.New([]byte("server-bot-test-secret"))
	if err != nil {
		t.Fatal(err)
	}
	game.demos = demos
	game.runners = &fakeRunners{database: database, tokens: tokens, demos: demos, failing: map[string]bool{}}
	sender := &services.TurnSender{Runners: game.runners, Packages: fakeLinks{}, Tokens: tokens, SubmitURL: serverBotSubmitURL,
		Origin: serverBotOrigin, Demos: services.DemoLinks{Tokens: demos}}
	publisher := &testutils.RecordedPublisher{}
	game.service = &services.GenerationService{Database: database, Publisher: publisher, Bots: sender}
	for _, record := range packages {
		record.LobbyID = game.lobbyID
		if err := dal.CreateBotPackage(database, record); err != nil {
			t.Fatal(err)
		}
	}
	snapshot := domain.Snapshot{Lobby: domain.Lobby{ID: game.lobbyID, GameKey: "island-battles", Phase: domain.PhaseStarted, GameSettings: testutils.CircleMapSettings(len(bots), `{"turn_timeout_seconds":30}`)}}
	chosen := map[string]services.ServerBot{}
	for index, bot := range bots {
		team := domain.Team{ID: uuid.NewString(), Order: index + 1, Name: fmt.Sprintf("Team %d", index+1)}
		snapshot.Teams = append(snapshot.Teams, team)
		player := domain.Principal{PlayerID: uuid.NewString()}
		game.players = append(game.players, player)
		snapshot.StartRoster = append(snapshot.StartRoster, domain.StartRosterEntry{PlayerID: player.PlayerID, TeamID: team.ID})
		snapshot.Members = append(snapshot.Members, domain.Member{PlayerID: player.PlayerID, DisplayName: fmt.Sprintf("Player %d", index+1)})
		if bot != (services.ServerBot{}) {
			chosen[player.PlayerID] = bot
		}
	}
	for _, index := range failing {
		game.runners.failing[game.players[index].PlayerID] = true
	}
	binding, err := services.RunLobbyGameWithServerBots(context.Background(), database, publisher, sender, snapshot, chosen)
	if err != nil {
		t.Fatal(err)
	}
	game.gameID = binding.GameID
	return game
}

// latestGeneration is the game's open generation.
func (game serverBotGame) latestGeneration(t *testing.T) *models.Generation {
	t.Helper()
	generation, err := dal.GetLatestGenerationForGame(game.database, game.gameID)
	if err != nil {
		t.Fatal(err)
	}
	return generation
}

// submit plays an event's turn as its bot would: it submits a pass with the
// event's submit token.
func (game serverBotGame) submit(t *testing.T, event models.BotRunnerEvent) models.GenerationState {
	t.Helper()
	claims, err := game.tokens.Verify(event.Delivery.Submit.Token, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	complete := true
	state, err := game.service.Register(context.Background(), domain.Principal{PlayerID: claims.PlayerID}, models.RegisterGenerationCommands{
		GameID: claims.GameID, GenerationID: claims.GenerationID, RequestID: uuid.NewString(), Complete: &complete, Commands: []models.PublicGenerationCommand{}})
	if err != nil {
		t.Fatal(err)
	}
	return state
}

func pythonPackage() models.BotPackage {
	packageID := uuid.NewString()
	uploaded := time.Now()
	return models.BotPackage{PackageID: packageID, OwnerPlayerID: uuid.NewString(), OwnerName: "Alex", Status: models.BotPackageReady,
		S3Key: "lobbies/l/" + packageID + ".zip", Language: "python", EntryOrCommand: "bot:play", SizeBytes: 4321, UploadedAt: &uploaded}
}

// playerOf is the player an event's submit token stands for.
func (game serverBotGame) playerOf(t *testing.T, event models.BotRunnerEvent) string {
	t.Helper()
	claims, err := game.tokens.Verify(event.Delivery.Submit.Token, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	return claims.PlayerID
}

func TestTheStartSendsTheFirstTurnOfEachHostedAndUploadedBot(t *testing.T) {
	uploaded := pythonPackage()
	game := startServerBotGame(t, []models.BotPackage{uploaded}, []services.ServerBot{
		{PackageID: uploaded.PackageID}, {URL: "https://bot.example.org/turn"}, {Level: 1}, {}})
	generation := game.latestGeneration(t)

	sent := game.runners.take()
	if len(sent) != 2 {
		t.Fatalf("sent %d turns, want one per hosted or uploaded bot: %+v", len(sent), sent)
	}
	byPlayer := map[string]invocation{}
	for _, invoked := range sent {
		if !invoked.committed {
			t.Error("a turn was sent before the generation that holds it committed")
		}
		delivery := invoked.event.Delivery
		if delivery.Turn.GameID != game.gameID || delivery.Turn.Generation.ID != generation.ID || delivery.Turn.TurnNumber != 1 ||
			delivery.Turn.MaxGenerations != models.DefaultMaxGenerations || delivery.Submit.URL != serverBotSubmitURL ||
			delivery.Submit.DeadlineAt == nil || delivery.Turn.TurnDeadlineAt == nil || !delivery.Submit.DeadlineAt.Equal(*delivery.Turn.TurnDeadlineAt) {
			t.Errorf("unexpected delivery %+v", delivery.Submit)
		}
		byPlayer[game.playerOf(t, invoked.event)] = invoked
	}

	python := byPlayer[game.players[0].PlayerID]
	if python.runner != models.BotRunnerPython || python.event.Hosted != nil || python.event.Package == nil {
		t.Fatalf("the uploaded bot's event = %+v", python)
	}
	wantPackage := models.BotRunnerPackage{ID: uploaded.PackageID, URL: "https://packages.example/" + uploaded.S3Key + "?expires=1h0m0s", SizeBytes: 4321}
	if *python.event.Package != wantPackage || python.event.Limits == nil || *python.event.Limits != (models.BotRunnerLimits{LoadSeconds: 10, PlaySeconds: 2}) {
		t.Errorf("package %+v, limits %+v", python.event.Package, python.event.Limits)
	}
	if python.event.Delivery.Turn.TeamColor != 1 {
		t.Errorf("the uploaded bot plays team %d", python.event.Delivery.Turn.TeamColor)
	}

	hosted := byPlayer[game.players[1].PlayerID]
	if hosted.runner != models.BotRunnerNative || hosted.event.Package != nil || hosted.event.Limits != nil ||
		hosted.event.Hosted == nil || hosted.event.Hosted.URL != "https://bot.example.org/turn" {
		t.Fatalf("the hosted bot's event = %+v", hosted)
	}
	// The delivery is what GET …/turn-delivery answers that player.
	served, err := game.service.TurnDelivery(context.Background(), game.players[1], game.lobbyID, "", game.tokens, serverBotSubmitURL)
	if err != nil {
		t.Fatal(err)
	}
	sentTurn, _ := json.Marshal(hosted.event.Delivery.Turn)
	servedTurn, _ := json.Marshal(served.Turn)
	if string(sentTurn) != string(servedTurn) {
		t.Errorf("sent turn %s\nserved turn %s", sentTurn, servedTurn)
	}
	// The event is the runners' shape, as backend/botrunner/testdata/event.json.
	encoded, _ := json.Marshal(python.event)
	var shape map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &shape); err != nil || len(shape) != 3 || shape["package"] == nil || shape["delivery"] == nil || shape["limits"] == nil {
		t.Errorf("event %s", encoded)
	}
}

func TestEachSubmissionSendsTheNextTurn(t *testing.T) {
	uploaded := pythonPackage()
	game := startServerBotGame(t, []models.BotPackage{uploaded}, []services.ServerBot{{PackageID: uploaded.PackageID}, {URL: "https://bot.example.org/turn"}})
	first := game.latestGeneration(t).ID
	sent := game.runners.take()
	for turn := range 3 {
		if len(sent) != 2 {
			t.Fatalf("turn %d: sent %d", turn, len(sent))
		}
		// The first bot's submission completes nothing yet; the second's
		// plays the turn, and sends the next one to both, after it commits.
		game.submit(t, sent[0].event)
		if pending := game.runners.take(); len(pending) != 0 {
			t.Fatalf("turn %d: a half-completed turn sent %d", turn, len(pending))
		}
		state := game.submit(t, sent[1].event)
		if state.GenerationID != first+uint(turn)+1 {
			t.Fatalf("turn %d: generation %d", turn, state.GenerationID)
		}
		sent = game.runners.take()
		for _, invoked := range sent {
			if invoked.event.Delivery.Turn.Generation.ID != state.GenerationID || invoked.event.Delivery.Turn.TurnNumber != turn+2 ||
				state.TurnNumber != turn+2 || !invoked.committed {
				t.Fatalf("turn %d: sent %+v", turn, invoked)
			}
		}
	}
}

func TestAFailedRollbackSendsNothing(t *testing.T) {
	game := startServerBotGame(t, nil, []services.ServerBot{{URL: "https://one.example.org/turn"}, {URL: "https://two.example.org/turn"}})
	sent := game.runners.take()
	before := game.latestGeneration(t).ID
	game.submit(t, sent[0].event)

	// The next query of the game log fails, after the turn was played and
	// its turns collected, so the transaction rolls back.
	var failing atomic.Bool
	if err := game.database.Callback().Query().Before("gorm:query").Register("test:fail_generation_logs", func(database *gorm.DB) {
		if failing.Load() && database.Statement.Table == "generation_logs" {
			_ = database.AddError(errors.New("injected failure"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	failing.Store(true)
	claims, _ := game.tokens.Verify(sent[1].event.Delivery.Submit.Token, time.Now())
	complete := true
	_, err := game.service.Register(context.Background(), domain.Principal{PlayerID: claims.PlayerID}, models.RegisterGenerationCommands{
		GameID: claims.GameID, GenerationID: claims.GenerationID, RequestID: uuid.NewString(), Complete: &complete, Commands: []models.PublicGenerationCommand{}})
	failing.Store(false)

	if err == nil || !strings.Contains(err.Error(), "injected failure") {
		t.Fatalf("err = %v, want the injected failure", err)
	}
	if latest := game.latestGeneration(t).ID; latest != before {
		t.Fatalf("the rolled back turn left generation %d", latest)
	}
	if sent := game.runners.take(); len(sent) != 0 {
		t.Fatalf("a rolled back generation sent %d turns", len(sent))
	}
	// The same batch, again, plays the turn and sends it.
	state := game.submit(t, sent[1].event)
	if sent := game.runners.take(); len(sent) != 2 || state.GenerationID == before || sent[0].event.Delivery.Turn.Generation.ID != state.GenerationID {
		t.Fatalf("sent %+v", sent)
	}
}

func TestAFailedInvocationLogsALineAndLeavesTheTurnToTheDeadline(t *testing.T) {
	uploaded := pythonPackage()
	game := startServerBotGame(t, []models.BotPackage{uploaded}, []services.ServerBot{{PackageID: uploaded.PackageID}, {URL: "https://bot.example.org/turn"}}, 1)
	generation := game.latestGeneration(t)
	if sent := game.runners.take(); len(sent) != 2 {
		t.Fatalf("both turns are tried: %d", len(sent))
	}
	lines, err := dal.LoadGenerationLogs(game.database, generation.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 1 || lines[0].PlayerID != game.players[1].PlayerID || lines[0].Level != models.GenerationLogError ||
		lines[0].Logger != "island_battles.bots" || lines[0].Message != "The server couldn't start your bot this turn." {
		t.Fatalf("lines = %+v", lines)
	}
	// The turn is still open for the bot, until its deadline.
	state, err := game.service.Current(context.Background(), game.players[1], game.lobbyID)
	if err != nil || state.Submitted || state.Outcome != nil || state.LogLinesStored != 1 {
		t.Fatalf("state %+v, %v", state, err)
	}
}

func TestAPackageDeletedBeforeTheFreezePassesEveryTurn(t *testing.T) {
	// The package isn't in the lobby, as when it was deleted between the
	// start's check and the freeze.
	game := startServerBotGame(t, nil, []services.ServerBot{{PackageID: uuid.NewString()}, {URL: "https://bot.example.org/turn"}})
	for turn := range 2 {
		generation := game.latestGeneration(t)
		sent := game.runners.take()
		if len(sent) != 1 || game.playerOf(t, sent[0].event) != game.players[1].PlayerID {
			t.Fatalf("turn %d: only the hosted bot gets its turn: %+v", turn, sent)
		}
		inputs, err := dal.LoadGenerationInputs(game.database, generation.ID)
		if err != nil {
			t.Fatal(err)
		}
		if !slices.ContainsFunc(inputs, func(input models.GenerationInput) bool {
			return input.PlayerID == game.players[0].PlayerID && input.Completed && string(input.Commands) == "[]"
		}) {
			t.Fatalf("turn %d: the seat without a package didn't pass: %+v", turn, inputs)
		}
		lines, err := dal.LoadGenerationLogs(game.database, generation.ID)
		if err != nil {
			t.Fatal(err)
		}
		if len(lines) != 1 || lines[0].PlayerID != game.players[0].PlayerID || !strings.Contains(lines[0].Message, "deleted before the game started") {
			t.Fatalf("turn %d: lines %+v", turn, lines)
		}
		game.submit(t, sent[0].event)
	}
}

func TestOnlyStandingUnfinishedServerBotsGetTheirTurn(t *testing.T) {
	sender := &services.TurnSender{Runners: &fakeRunners{}, Tokens: mustSigner(t), SubmitURL: serverBotSubmitURL}
	key, url := "lobbies/l/p.zip", "https://bot.example.org/turn"
	level := uint(1)
	players := []models.FrozenGamePlayer{
		{PlayerID: "hosted", TeamColor: 1, BotKind: models.HostedBot, BotURL: &url},
		{PlayerID: "uploaded", TeamColor: 2, BotKind: models.UploadedBot, BotPackageKey: &key},
		{PlayerID: "eliminated", TeamColor: 3, BotKind: models.HostedBot, BotURL: &url},
		{PlayerID: "completed", TeamColor: 4, BotKind: models.HostedBot, BotURL: &url},
		{PlayerID: "no package", TeamColor: 5, BotKind: models.UploadedBot},
		{PlayerID: "default", TeamColor: 6, BotKind: models.DefaultBot, BotLevel: &level},
		{PlayerID: "localhost", TeamColor: 7, BotKind: models.LocalhostBot},
	}
	game := &models.Game{Model: gorm.Model{ID: 7}}
	// Numbered as the DAL's loaders number a game's generations.
	generation := &models.Generation{Model: gorm.Model{ID: 41, CreatedAt: time.Now()}, Number: 3}
	for color := uint(1); color <= 7; color++ {
		game.Teams = append(game.Teams, models.GameTeam{Color: color})
		// Team 3 has nothing left on the board.
		if color != 3 {
			generation.Map.Castles = append(generation.Map.Castles, models.Castle{CastleFields: models.CastleFields{TeamColor: color, Upgrades: models.Upgrades{}.Clone()}})
		}
	}
	inputs := []models.GenerationInput{{GenerationID: 41, PlayerID: "completed", Completed: true, Commands: []byte("[]")}, {GenerationID: 41, PlayerID: "uploaded", Commands: []byte("[]")}}

	sent, err := services.SentTurnPlayers(sender, game, generation, players, inputs)
	if err != nil || !slices.Equal(sent, []string{"hosted", "uploaded"}) {
		t.Fatalf("sent %v, %v", sent, err)
	}
	// A finished game sends every hosted and uploaded bot the end of its
	// game instead, whatever became of its team (#413). A server without a
	// sender sends nothing.
	game.Outcome = &models.GameOutcome{Reason: models.GameOutcomeLastTeamStanding}
	if sent, err := services.SentTurnPlayers(sender, game, generation, players, inputs); err != nil || !slices.Equal(sent, []string{"hosted", "uploaded", "eliminated", "completed"}) {
		t.Fatalf("a finished game sent %v, %v", sent, err)
	}
	game.Outcome = nil
	if sent, err := services.SentTurnPlayers(nil, game, generation, players, inputs); err != nil || len(sent) != 0 {
		t.Fatalf("without a sender: %v, %v", sent, err)
	}
}

func mustSigner(t *testing.T) *submittoken.Signer {
	t.Helper()
	signer, err := submittoken.New([]byte("secret"))
	if err != nil {
		t.Fatal(err)
	}
	return signer
}

// When a game ends, every hosted and uploaded bot on its roster is told,
// once the outcome has committed, with a demo link that downloads the game's
// full demo as its player sees it, and works for no other game (#413).
func TestTheEndOfAGameIsSentToEveryServerRunBot(t *testing.T) {
	uploaded := pythonPackage()
	// The third player's localhost bot never answers, so the turn times out.
	game := startServerBotGame(t, []models.BotPackage{uploaded}, []services.ServerBot{{PackageID: uploaded.PackageID}, {URL: "https://bot.example.org/bots/turn"}, {}})
	for _, invoked := range game.runners.take() {
		game.submit(t, invoked.event)
	}
	if pending := game.runners.take(); len(pending) != 0 {
		t.Fatalf("a turn that waits for the localhost bot sent %d", len(pending))
	}
	final := game.latestGeneration(t)
	game.service.Now = func() time.Time { return time.Now().Add(time.Minute) }
	state, err := game.service.ApplyDeadline(context.Background(), game.players[2], game.lobbyID)
	if err != nil || state.Outcome == nil || state.Outcome.Reason != models.GameOutcomeTurnTimeout {
		t.Fatalf("the deadline: %+v, %v", state.Outcome, err)
	}
	sent := game.runners.take()
	if len(sent) != 2 {
		t.Fatalf("sent %d events, want one per server-run bot", len(sent))
	}
	byPlayer := map[string]invocation{}
	for _, invoked := range sent {
		ended := invoked.event.GameEnded
		if ended == nil || invoked.event.Delivery != nil || !invoked.committed {
			t.Fatalf("sent %+v", invoked)
		}
		claims, err := demoClaims(game.demos, ended.Demo)
		if err != nil || claims.GameID != game.gameID {
			t.Fatalf("the demo link %q: %+v, %v", ended.Demo.URL, claims, err)
		}
		wantURL := fmt.Sprintf("%s/game/%d/demo?token=", serverBotOrigin, game.gameID)
		if !strings.HasPrefix(ended.Demo.URL, wantURL) || !ended.Demo.ExpiresAt.Equal(final.CreatedAt.Add(services.DefaultDemoLinkLifetime).UTC().Truncate(time.Second)) {
			t.Fatalf("the demo link %+v", ended.Demo)
		}
		if ended.GameID != game.gameID || ended.TurnNumber != final.Number || ended.Generation.ID != final.ID || ended.Outcome.Reason != models.GameOutcomeTurnTimeout {
			t.Fatalf("game_ended %+v", ended)
		}
		byPlayer[claims.PlayerID] = invoked
	}
	uploadedEvent, hostedEvent := byPlayer[game.players[0].PlayerID], byPlayer[game.players[1].PlayerID]
	if uploadedEvent.runner != models.BotRunnerPython || uploadedEvent.event.Package == nil || uploadedEvent.event.Limits == nil || *uploadedEvent.event.Limits != models.GameEndedBotRunnerLimits {
		t.Fatalf("the uploaded bot's event %+v", uploadedEvent)
	}
	if hostedEvent.runner != models.BotRunnerNative || hostedEvent.event.Hosted == nil || hostedEvent.event.Hosted.URL != "https://bot.example.org/bots/turn" {
		t.Fatalf("the hosted bot's event %+v", hostedEvent)
	}

	// The demo token stands for its player on its game, and the demo holds
	// every team's logs and the outcome.
	claims, _ := demoClaims(game.demos, hostedEvent.event.GameEnded.Demo)
	principal, err := game.service.AuthorizeDemoToken(context.Background(), claims)
	if err != nil || principal.PlayerID != game.players[1].PlayerID {
		t.Fatalf("authorize: %+v, %v", principal, err)
	}
	demo, err := game.service.GameDemo(context.Background(), principal, game.gameID)
	if err != nil || demo.Outcome == nil || demo.GameID != game.gameID || len(demo.Frames) != final.Number {
		t.Fatalf("the demo: %d frames, outcome %+v, %v", len(demo.Frames), demo.Outcome, err)
	}
	for _, frame := range demo.Logs {
		if len(frame.HiddenTeamColors) != 0 {
			t.Fatalf("a finished game's demo hides teams: %+v", frame.HiddenTeamColors)
		}
	}
	claims.PlayerID = uuid.NewString()
	if _, err := game.service.AuthorizeDemoToken(context.Background(), claims); !errors.Is(err, demotoken.ErrInvalid) {
		t.Fatalf("a player off the roster: %v", err)
	}

	// The localhost bot's client reads the same end, for its bot.
	links := services.DemoLinks{Tokens: game.demos}
	ended, err := game.service.GameEndedDelivery(context.Background(), game.players[2], game.lobbyID, "", links, serverBotOrigin)
	if err != nil || ended.TeamColor != 3 || ended.Outcome.Reason != models.GameOutcomeTurnTimeout {
		t.Fatalf("the localhost bot's end: %+v, %v", ended, err)
	}
	if claims, err := demoClaims(game.demos, ended.Demo); err != nil || claims.PlayerID != game.players[2].PlayerID {
		t.Fatalf("its demo link: %+v, %v", claims, err)
	}
	// Asking again sends nothing more.
	if _, err := game.service.ApplyDeadline(context.Background(), game.players[2], game.lobbyID); err != nil {
		t.Fatal(err)
	}
	if again := game.runners.take(); len(again) != 0 {
		t.Fatalf("the end was sent again: %d", len(again))
	}
}

// A running game's end can't be read yet.
func TestTheEndOfARunningGameIsAConflict(t *testing.T) {
	game := startServerBotGame(t, nil, []services.ServerBot{{URL: "https://bot.example.org/turn"}, {}})
	_, err := game.service.GameEndedDelivery(context.Background(), game.players[1], game.lobbyID, "", services.DemoLinks{Tokens: game.demos}, serverBotOrigin)
	var rejection *services.GenerationError
	if !errors.As(err, &rejection) || rejection.Status != 409 || rejection.Type != services.GameNotEndedProblemType {
		t.Fatalf("got %v", err)
	}
}
