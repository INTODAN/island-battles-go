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

	"islandBattles/backend/dal"
	"islandBattles/backend/internal/testutils"
	"islandBattles/backend/models"
	"islandBattles/backend/services"
	"islandBattles/backend/submittoken"
	"islandBattles/lobby/domain"

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
	mutex    sync.Mutex
	events   []invocation
	failing  map[string]bool
}

func (runners *fakeRunners) Invoke(ctx context.Context, runner models.BotRunnerName, event models.BotRunnerEvent) error {
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

const serverBotSubmitURL = "https://island-battles.example.org/generation/commands"

// serverBotGame is a started game with bots that the server plays.
type serverBotGame struct {
	database *gorm.DB
	runners  *fakeRunners
	tokens   *submittoken.Signer
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
	game.runners = &fakeRunners{database: database, tokens: tokens, failing: map[string]bool{}}
	sender := &services.TurnSender{Runners: game.runners, Packages: fakeLinks{}, Tokens: tokens, SubmitURL: serverBotSubmitURL}
	publisher := &testutils.RecordedPublisher{}
	game.service = &services.GenerationService{Database: database, Publisher: publisher, Bots: sender}
	for _, record := range packages {
		record.LobbyID = game.lobbyID
		if err := dal.CreateBotPackage(database, record); err != nil {
			t.Fatal(err)
		}
	}
	snapshot := domain.Snapshot{Lobby: domain.Lobby{ID: game.lobbyID, GameKey: "island-battles", Phase: domain.PhaseStarted, GameSettings: json.RawMessage(`{"turn_timeout_seconds":30}`)}}
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
		if delivery.Turn.GameID != game.gameID || delivery.Turn.Generation.ID != generation.ID || delivery.Submit.URL != serverBotSubmitURL ||
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
	served, err := game.service.TurnDelivery(context.Background(), game.players[1], game.lobbyID, game.tokens, serverBotSubmitURL)
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
			if invoked.event.Delivery.Turn.Generation.ID != state.GenerationID || !invoked.committed {
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
	generation := &models.Generation{Model: gorm.Model{ID: 41, CreatedAt: time.Now()}}
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
	// A finished game sends nothing, nor does a server without a sender.
	game.Outcome = &models.GameOutcome{Reason: models.GameOutcomeLastTeamStanding}
	if sent, err := services.SentTurnPlayers(sender, game, generation, players, inputs); err != nil || len(sent) != 0 {
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
