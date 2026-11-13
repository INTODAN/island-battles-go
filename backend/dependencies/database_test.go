package dependencies_test

import (
	"bytes"
	"context"
	"encoding/json"
	"islandBattles/backend/schema"
	"log/slog"
	"net"
	"strings"
	"testing"
	"time"

	"islandBattles/backend/dal"
	"islandBattles/backend/dependencies"
	"islandBattles/backend/internal/testutils"
	"islandBattles/backend/models"

	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace/noop"
	gormpostgres "gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func databaseURL(t *testing.T, db *gorm.DB) string {
	t.Helper()
	dialector, ok := db.Dialector.(*gormpostgres.Dialector)
	if !ok {
		t.Fatalf("unexpected dialector %T", db.Dialector)
	}
	return dialector.DSN
}

func connect(t *testing.T, config dependencies.DatabaseConfig, logger *slog.Logger) *gorm.DB {
	t.Helper()
	connected, err := dependencies.InitDB(config, logger)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if sqlDatabase, err := connected.DB(); err == nil {
			_ = sqlDatabase.Close()
		}
	})
	return connected
}

// Generation events are pushed after commit, so migrating drops the table
// that queued them in databases created before.
func TestMigrateSchemaDropsTheGenerationNotificationQueue(t *testing.T) {
	db := testutils.InitTestDB(t)
	if err := db.Exec(`CREATE TABLE generation_notifications (id bigserial PRIMARY KEY, payload jsonb)`).Error; err != nil {
		t.Fatal(err)
	}
	if err := schema.Migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	if db.Migrator().HasTable("generation_notifications") {
		t.Fatal("generation_notifications survived migrating")
	}
}

// Each person's bot is in their lobby member settings, so migrating drops the
// table of bot choices, with its foreign key to the lobby's memberships, and
// its choices, in databases created before.
func TestMigrateSchemaDropsTheBotChoiceTable(t *testing.T) {
	db := testutils.InitTestDB(t)
	err := db.Exec(`CREATE TABLE island_lobby_bot_choices (lobby_id uuid, player_id uuid, kind text NOT NULL, level bigint, created_at timestamptz, updated_at timestamptz, PRIMARY KEY (lobby_id, player_id),
CONSTRAINT fk_island_lobby_bot_choices_membership FOREIGN KEY (lobby_id, player_id) REFERENCES lobby_memberships (lobby_id, player_id) ON DELETE CASCADE)`).Error
	if err != nil {
		t.Fatal(err)
	}
	if err := schema.Migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	if db.Migrator().HasTable("island_lobby_bot_choices") {
		t.Fatal("island_lobby_bot_choices survived migrating")
	}
	if !db.Migrator().HasColumn("lobby_memberships", "member_settings") {
		t.Fatal("lobby_memberships has no member_settings")
	}
}

// frozenPlayerBeforeBotKinds is the frozen_game_players table from before
// bot kinds, when a NULL bot level meant the localhost bot.
type frozenPlayerBeforeBotKinds struct {
	GameID      uint   `gorm:"primaryKey;autoIncrement:false"`
	PlayerID    string `gorm:"type:uuid;primaryKey"`
	TeamColor   uint   `gorm:"not null"`
	DisplayName string `gorm:"not null;default:''"`
	BotLevel    *uint
	CreatedAt   time.Time
}

func (frozenPlayerBeforeBotKinds) TableName() string { return "frozen_game_players" }

// Migrating gives the frozen players stored before bot kinds their kind:
// default where a level is set, localhost otherwise. Later runs keep the
// kinds that games store.
func TestMigrateSchemaBackfillsTheFrozenPlayersBotKinds(t *testing.T) {
	db := testutils.InitEmptyTestDB(t)
	if err := db.AutoMigrate(&frozenPlayerBeforeBotKinds{}); err != nil {
		t.Fatal(err)
	}
	level := uint(3)
	const localhostPlayer, defaultPlayer = "00000000-0000-4000-8000-000000000001", "00000000-0000-4000-8000-000000000002"
	if err := db.Create(&[]frozenPlayerBeforeBotKinds{{GameID: 1, PlayerID: localhostPlayer, TeamColor: 1}, {GameID: 1, PlayerID: defaultPlayer, TeamColor: 2, BotLevel: &level}}).Error; err != nil {
		t.Fatal(err)
	}
	if err := schema.Migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	// A game started since stores a hosted bot, which another run keeps.
	url := "https://bot.example.com/turn"
	if err := db.Create(&models.FrozenGamePlayer{GameID: 2, PlayerID: localhostPlayer, TeamColor: 1, BotKind: models.HostedBot, BotURL: &url}).Error; err != nil {
		t.Fatal(err)
	}
	if err := schema.Migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	players, err := dal.LoadFrozenPlayers(db, 1)
	if err != nil || len(players) != 2 {
		t.Fatalf("players = %+v, %v", players, err)
	}
	for _, player := range players {
		want := models.LocalhostBot
		if player.PlayerID == defaultPlayer {
			want = models.DefaultBot
		}
		if player.BotKind != want {
			t.Errorf("player %s: kind %q, want %q", player.PlayerID, player.BotKind, want)
		}
		if bot := models.FrozenBot(player); bot.Kind != want || (want == models.DefaultBot) != (bot.Level != nil) {
			t.Errorf("player %s: GenerationState.bot = %+v", player.PlayerID, bot)
		}
	}
	hosted, err := dal.LoadFrozenPlayers(db, 2)
	if err != nil || len(hosted) != 1 || hosted[0].BotKind != models.HostedBot || models.FrozenBot(hosted[0]).Host != "bot.example.com" {
		t.Fatalf("hosted = %+v, %v", hosted, err)
	}
}

// gameWithReplay is the games table from before replays were derived from
// generations.
type gameWithReplay struct {
	gorm.Model
	LobbyID            *string `gorm:"type:uuid;uniqueIndex"`
	Replay             []byte  `gorm:"type:jsonb;not null"`
	NextSoldierGroupID uint    `gorm:"not null"`
}

func (gameWithReplay) TableName() string { return "games" }

func TestMigrateSchemaDropsTheOldReplayColumn(t *testing.T) {
	db := testutils.InitEmptyTestDB(t)
	if err := db.AutoMigrate(&gameWithReplay{}); err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&gameWithReplay{Replay: []byte(`{"frames":[]}`)}).Error; err != nil {
		t.Fatal(err)
	}
	// The second run finds the column already gone.
	for range 2 {
		if err := schema.Migrate(context.Background(), db); err != nil {
			t.Fatal(err)
		}
	}
	if db.Migrator().HasColumn(&models.Game{}, "replay") {
		t.Fatal("games still has the replay column")
	}
	if _, err := dal.CreateGame(db); err != nil {
		t.Fatalf("new games cannot be stored: %v", err)
	}
}

// A Lambda cold start connects without touching the schema, and its pool
// stays within the configured bounds.
func TestInitDBRunsNoDDLAndBoundsThePool(t *testing.T) {
	empty := testutils.InitEmptyTestDB(t)
	config := dependencies.DefaultDatabaseConfig(databaseURL(t, empty))
	config.MaxOpenConnections, config.MaxIdleConnections, config.ConnectionMaxIdleTime = 3, 1, 30*time.Second
	connected := connect(t, config, slog.Default())
	tables, err := connected.Migrator().GetTables()
	if err != nil {
		t.Fatal(err)
	}
	if len(tables) != 0 {
		t.Fatalf("connecting created tables %v", tables)
	}
	sqlDatabase, err := connected.DB()
	if err != nil {
		t.Fatal(err)
	}
	if limit := sqlDatabase.Stats().MaxOpenConnections; limit != 3 {
		t.Fatalf("max open connections = %d, want 3", limit)
	}
	// database/sql reports no idle limit, so hold three connections and check
	// that only one stays idle once they are returned.
	connections := make([]interface{ Close() error }, 0, 3)
	for range 3 {
		connection, err := sqlDatabase.Conn(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		connections = append(connections, connection)
	}
	for _, connection := range connections {
		if err := connection.Close(); err != nil {
			t.Fatal(err)
		}
	}
	if idle := sqlDatabase.Stats().Idle; idle != 1 {
		t.Fatalf("idle connections = %d, want 1", idle)
	}
}

// GORM's own logger writes coloured text, which CloudWatch can't query, so
// failed and slow queries must go through the host's slog logger. Both are
// warnings: a request that fails logs its own error line, and the error alarm
// must not fire on queries that fail as expected, such as unique violations.
func TestInitDBLogsFailedAndSlowQueriesThroughSlog(t *testing.T) {
	empty := testutils.InitEmptyTestDB(t)
	var output bytes.Buffer
	connected := connect(t, dependencies.DefaultDatabaseConfig(databaseURL(t, empty)), slog.New(slog.NewJSONHandler(&output, nil)))
	if err := connected.Exec(`SELECT * FROM missing_table WHERE name = ?`, "secret-value").Error; err == nil {
		t.Fatal("query on a missing table succeeded")
	}
	slowQuery := `SELECT pg_sleep(?)`
	if err := connected.Exec(slowQuery, (dependencies.SlowQueryThreshold + 50*time.Millisecond).Seconds()).Error; err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(output.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("logged %d lines, want 2: %s", len(lines), output.String())
	}
	for index, want := range []struct{ level, query string }{{"WARN", "missing_table"}, {"WARN", "pg_sleep"}} {
		var line map[string]any
		if err := json.Unmarshal([]byte(lines[index]), &line); err != nil {
			t.Fatalf("line %d is not JSON: %s", index, lines[index])
		}
		if line["level"] != want.level || !strings.Contains(lines[index], want.query) {
			t.Fatalf("line %d = %s, want a %s line about %s", index, lines[index], want.level, want.query)
		}
	}
	if strings.Contains(output.String(), "secret-value") {
		t.Fatalf("query values were logged: %s", output.String())
	}
}

// A database that accepts the connection and never answers must not hold a
// Lambda cold start past Lambda's 10 seconds for init.
func TestInitDBGivesUpOnADatabaseThatNeverAnswers(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = listener.Close() }()
	go func() {
		for {
			connection, err := listener.Accept()
			if err != nil {
				return
			}
			t.Cleanup(func() { _ = connection.Close() })
		}
	}()
	start := time.Now()
	_, err = dependencies.InitDB(dependencies.DefaultDatabaseConfig("postgres://user:password@"+listener.Addr().String()+"/island_battles?sslmode=disable"), slog.New(slog.DiscardHandler))
	if elapsed := time.Since(start); err == nil || elapsed > dependencies.ConnectTimeout+2*time.Second {
		t.Fatalf("InitDB() = %v after %s", err, elapsed)
	}
}

// Each query inside a sampled trace gets a span, without the query's values.
func TestInitDBTracesQueriesInsideSampledTraces(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	otel.SetTracerProvider(provider)
	t.Cleanup(func() { otel.SetTracerProvider(noop.NewTracerProvider()) })
	empty := testutils.InitEmptyTestDB(t)
	connected := connect(t, dependencies.DefaultDatabaseConfig(databaseURL(t, empty)), slog.New(slog.DiscardHandler))
	ctx, request := provider.Tracer("test").Start(context.Background(), "request")
	if err := connected.WithContext(ctx).Exec(`SELECT ?::text`, "secret-value").Error; err != nil {
		t.Fatal(err)
	}
	request.End()
	var query sdktrace.ReadOnlySpan
	for _, span := range recorder.Ended() {
		if span.Parent().SpanID() == request.SpanContext().SpanID() {
			query = span
		}
	}
	if query == nil {
		t.Fatalf("no query span under the request among %d spans", len(recorder.Ended()))
	}
	for _, attribute := range query.Attributes() {
		if strings.Contains(attribute.Value.String(), "secret-value") {
			t.Fatalf("the query span holds its values: %s=%s", attribute.Key, attribute.Value.String())
		}
	}
}

// Migrating twice is safe, as every deploy runs the migrate command again.
func TestMigrateSchemaIsRepeatable(t *testing.T) {
	db := testutils.InitTestDB(t)
	if err := schema.Migrate(context.Background(), connect(t, dependencies.DefaultDatabaseConfig(databaseURL(t, db)), slog.Default())); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"lobby_rooms", "map_templates", "games", "generations"} {
		if !db.Migrator().HasTable(table) {
			t.Fatalf("missing table %s", table)
		}
	}
}
