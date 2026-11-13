// Package schema migrates Island Battles' database: the map templates and the
// game tables. The lobby keeps its tables in a database of its own, which the
// lobby's migrate command (github.com/INTODAN/mpl/cmd/migrate) migrates. The
// map templates' migrations run on the lobby's migration runner, and the check before the
// lobby's old tables are dropped reads the lobby's migration history, so this
// package may use the lobby's package postgres.
package schema

import (
	"context"
	"fmt"

	lobbypostgres "github.com/INTODAN/mpl/postgres"
	"islandBattles/backend/dal"
	"islandBattles/backend/dependencies"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Migrate brings the database to the schema this build expects. The migrate
// command runs it once per deploy, before any host serves requests. It runs
// the map template migrations, which keep their rows, and then AutoMigrates
// the game tables, whose data may be reset. Game tables come last so that
// they may reference templates.
func Migrate(ctx context.Context, db *gorm.DB) error {
	if err := MapTemplateMigrations().Apply(ctx, db); err != nil {
		return fmt.Errorf("migrate map template schema: %w", err)
	}
	return dependencies.MigrateGameSchema(ctx, db)
}

// MapTemplateMigrations is the versioned schema of the map templates,
// dal.MapTemplateMigrationFiles, run by the lobby's migration runner, which
// takes over a database created before versioning.
func MapTemplateMigrations() lobbypostgres.MigrationSet {
	return lobbypostgres.MigrationSet{HistoryTable: dal.MapTemplateHistoryTable, Files: dal.MapTemplateMigrationFiles(), Adopt: true}
}

// LobbyTablesCheck is CheckLobbyTables' answer: whether the lobby's tables in
// Island Battles' database may be dropped, and why or why not.
type LobbyTablesCheck struct {
	Drop   bool
	Reason string
}

// databaseIdentity is what a server says about the database a connection
// reached.
type databaseIdentity struct {
	Database string
	// System is the cluster's system identifier, which pg_control_system()
	// gives, or empty when it can't be read.
	System string
	// Address and Port are the server's, as the connection reached it; empty
	// over a Unix socket.
	Address string
	Port    string
}

// identify asks the server which database database reaches. The cluster's
// system identifier needs no superuser on PostgreSQL 10 and newer; without
// it, the server's address and port say less, and the caller treats what it
// can't tell as the same database.
func identify(ctx context.Context, database *gorm.DB) (databaseIdentity, error) {
	var identity databaseIdentity
	if err := database.WithContext(ctx).Raw(`SELECT current_database() AS database,
		coalesce(host(inet_server_addr()), '') AS address, coalesce(inet_server_port()::text, '') AS port`).
		Scan(&identity).Error; err != nil {
		return databaseIdentity{}, err
	}
	var system []string
	if err := database.WithContext(ctx).Raw(`SELECT system_identifier::text FROM pg_control_system()`).Scan(&system).Error; err == nil && len(system) == 1 {
		identity.System = system[0]
	}
	return identity, nil
}

// distinct reports whether two identities are provably two databases: their
// names differ, or both clusters' system identifiers are known and differ.
// Everything else, such as one name on two addresses that may be one server,
// counts as one database.
func distinct(game, lobby databaseIdentity) bool {
	if game.Database != lobby.Database {
		return true
	}
	return game.System != "" && lobby.System != "" && game.System != lobby.System
}

// CheckLobbyTables decides whether DropLobbyTables may run on game, Island
// Battles' database, now that lobby is the lobby's own (LOBBY_DATABASE_URL).
// It asks both servers rather than comparing URLs, since two URLs can reach
// one database (localhost and 127.0.0.1, a name and its address, a pooler).
// It allows the drop only when the two are provably different databases and
// the lobby's database has every lobby migration of this build applied, so a
// lobby migrate command that failed or didn't run is never followed by the
// drop.
func CheckLobbyTables(ctx context.Context, game, lobby *gorm.DB) (LobbyTablesCheck, error) {
	gameIdentity, err := identify(ctx, game)
	if err != nil {
		return LobbyTablesCheck{}, fmt.Errorf("identify Island Battles' database: %w", err)
	}
	lobbyIdentity, err := identify(ctx, lobby)
	if err != nil {
		return LobbyTablesCheck{}, fmt.Errorf("identify the lobby's database: %w", err)
	}
	if !distinct(gameIdentity, lobbyIdentity) {
		return LobbyTablesCheck{Reason: fmt.Sprintf("%s reaches %q, which can't be told apart from Island Battles' database, so the lobby's tables there stay",
			dependencies.LobbyDatabaseURLEnv, lobbyIdentity.Database)}, nil
	}
	current, err := lobbypostgres.Migrations().Current(ctx, lobby)
	if err != nil {
		return LobbyTablesCheck{}, fmt.Errorf("read the lobby's migrations: %w", err)
	}
	if !current {
		return LobbyTablesCheck{Reason: fmt.Sprintf("the lobby's database %q lacks this build's lobby migrations, so the lobby's tables in Island Battles' database stay; run github.com/INTODAN/mpl/cmd/migrate, then this command again",
			lobbyIdentity.Database)}, nil
	}
	return LobbyTablesCheck{Drop: true, Reason: fmt.Sprintf("the lobby's own database %q is migrated", lobbyIdentity.Database)}, nil
}

// DropLobbyTables drops the lobby's tables, every lobby_* table with its
// migration history lobby_schema_migrations, from Island Battles' database,
// where builds before the lobby's own database kept them. Run it only when
// CheckLobbyTables allows it. Lobbies open at that moment are dropped with
// them, as the owner decided (#229); games already started keep their own
// data. No game table references them. It runs in one transaction, and finds
// nothing to do the next time.
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
