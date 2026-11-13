// Package schema migrates Island Battles' database: the map templates and the
// game tables. The lobby keeps its tables in a database of its own, which the
// lobby's migrate command (lobby/cmd/migrate) migrates. The map templates'
// migrations run on the lobby's migration runner, so this package may use
// lobby/postgres.
package schema

import (
	"context"
	"fmt"
	"net"

	"islandBattles/backend/dal"
	"islandBattles/backend/dependencies"
	lobbypostgres "islandBattles/lobby/postgres"

	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Options say what Migrate does besides migrating.
type Options struct {
	// SeparateLobbyDatabase says that the lobby keeps its tables in a
	// database of its own, so the copies that builds before it left in this
	// database are dropped. Only then: until the lobby has a database of its
	// own, its tables here are the only ones it has. See
	// SeparateLobbyDatabase.
	SeparateLobbyDatabase bool
}

// Migrate brings the database to the schema this build expects. The migrate
// command runs it once per deploy, before any host serves requests. It runs
// the map template migrations, which keep their rows, and then AutoMigrates
// the game tables, whose data may be reset. Game tables come last so that
// they may reference templates. With options.SeparateLobbyDatabase, it then
// drops the lobby's tables (DropLobbyTables).
func Migrate(ctx context.Context, db *gorm.DB, options Options) error {
	if err := MapTemplateMigrations().Apply(ctx, db); err != nil {
		return fmt.Errorf("migrate map template schema: %w", err)
	}
	if err := dependencies.MigrateGameSchema(ctx, db); err != nil {
		return err
	}
	if options.SeparateLobbyDatabase {
		return DropLobbyTables(ctx, db)
	}
	return nil
}

// MapTemplateMigrations is the versioned schema of the map templates,
// dal.MapTemplateMigrationFiles, run by the lobby's migration runner, which
// takes over a database created before versioning.
func MapTemplateMigrations() lobbypostgres.MigrationSet {
	return lobbypostgres.MigrationSet{HistoryTable: dal.MapTemplateHistoryTable, Files: dal.MapTemplateMigrationFiles(), Adopt: true}
}

// DropLobbyTables drops the lobby's tables, every lobby_* table with its
// migration history lobby_schema_migrations, from Island Battles' database,
// where builds before the lobby's own database kept them. Lobbies open at
// that moment are dropped with them, as the owner decided (#229); games
// already started keep their own data. No game table references them. It
// runs in one transaction, and finds nothing to do the next time.
func DropLobbyTables(ctx context.Context, db *gorm.DB) error {
	return db.WithContext(ctx).Transaction(func(transaction *gorm.DB) error {
		var tables []string
		if err := transaction.Raw(`SELECT tablename FROM pg_tables WHERE schemaname = current_schema() AND tablename LIKE 'lobby\_%' ORDER BY tablename`).
			Scan(&tables).Error; err != nil {
			return fmt.Errorf("list the lobby's tables: %w", err)
		}
		for _, table := range tables {
			// The lobby's tables reference each other, so CASCADE drops the
			// foreign keys between them; nothing else references them.
			if err := transaction.Exec(`DROP TABLE IF EXISTS ? CASCADE`, clause.Table{Name: table}).Error; err != nil {
				return fmt.Errorf("drop %s: %w", table, err)
			}
		}
		return nil
	})
}

// SeparateLobbyDatabase reports whether lobbyURL, the lobby's
// LOBBY_DATABASE_URL, names another database than gameURL, Island Battles'
// ISLAND_BATTLES_DATABASE_URL: another database name, host or port. An empty
// lobbyURL names none, so the lobby's tables stay where they are.
func SeparateLobbyDatabase(gameURL, lobbyURL string) (bool, error) {
	if lobbyURL == "" {
		return false, nil
	}
	game, err := pgconn.ParseConfig(gameURL)
	if err != nil {
		return false, fmt.Errorf("%s: %w", dependencies.DatabaseURLEnv, err)
	}
	lobby, err := pgconn.ParseConfig(lobbyURL)
	if err != nil {
		return false, fmt.Errorf("%s: %w", dependencies.LobbyDatabaseURLEnv, err)
	}
	sameServer := sameHost(game.Host, lobby.Host) && game.Port == lobby.Port
	return !sameServer || game.Database != lobby.Database, nil
}

// sameHost compares host names and addresses; localhost and the loopback
// addresses are one host.
func sameHost(first, second string) bool {
	loopback := func(host string) bool {
		if host == "localhost" {
			return true
		}
		address := net.ParseIP(host)
		return address != nil && address.IsLoopback()
	}
	return first == second || loopback(first) && loopback(second)
}
