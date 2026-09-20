// Package migrations owns the one-time import of backend legacy schema. The
// generic lobby package never needs to know about games or historical players.
package migrations

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"
	lobbypostgres "islandBattles/lobby/postgres"
)

const legacyLobbyVersion int64 = 1
const legacyLobbyLock int64 = 741621856

type Report struct {
	Players        int64  `json:"players"`
	Games          int64  `json:"games"`
	Checksum       string `json:"checksum"`
	AlreadyApplied bool   `json:"already_applied"`
}

type migrationRecord struct {
	Version   int64     `gorm:"primaryKey"`
	Players   int64     `gorm:"not null"`
	Games     int64     `gorm:"not null"`
	Checksum  string    `gorm:"not null"`
	AppliedAt time.Time `gorm:"not null"`
}

func (migrationRecord) TableName() string { return "island_lobby_migrations" }

// MigrateLegacy is additive and transactional. Stop the old application before
// invoking it. Completed imports never expand to include post-cutover games.
func MigrateLegacy(ctx context.Context, database *gorm.DB) (Report, error) {
	var report Report
	err := database.WithContext(ctx).Transaction(func(transaction *gorm.DB) error {
		if err := transaction.Exec("SELECT pg_advisory_xact_lock(?)", legacyLobbyLock).Error; err != nil {
			return err
		}
		if !transaction.Migrator().HasTable(&lobbypostgres.LegacyRecord{}) {
			return fmt.Errorf("generic lobby archive schema must be initialized first")
		}
		if err := transaction.AutoMigrate(&migrationRecord{}); err != nil {
			return err
		}
		var previous []migrationRecord
		if err := transaction.Where(&migrationRecord{Version: legacyLobbyVersion}).Limit(1).Find(&previous).Error; err != nil {
			return err
		}
		if len(previous) != 0 {
			report = Report{Players: previous[0].Players, Games: previous[0].Games, Checksum: previous[0].Checksum, AlreadyApplied: true}
			return nil
		}
		sources := []struct{ table, query string }{
			{"players", "SELECT id::text AS source_primary_key, to_jsonb(players)::text AS original_row_json FROM players ORDER BY id"},
			{"games", "SELECT id::text AS source_primary_key, jsonb_build_object('id', id, 'room_name', room_name, 'created_at', created_at, 'updated_at', updated_at, 'deleted_at', deleted_at)::text AS original_row_json FROM games ORDER BY id"},
		}
		digest := sha256.New()
		for _, source := range sources {
			if !transaction.Migrator().HasTable(source.table) {
				continue
			}
			if source.table == "games" && !transaction.Migrator().HasColumn("games", "room_name") {
				continue
			}
			var rows []struct {
				SourcePrimaryKey string
				OriginalRowJSON  string
			}
			if err := transaction.Raw(source.query).Scan(&rows).Error; err != nil {
				return err
			}
			for _, row := range rows {
				checksum := sha256.Sum256([]byte(row.OriginalRowJSON))
				checksumText := hex.EncodeToString(checksum[:])
				var archived []lobbypostgres.LegacyRecord
				filter := lobbypostgres.LegacyRecord{SourceTable: source.table, SourcePrimaryKey: row.SourcePrimaryKey}
				if err := transaction.Where(&filter).Limit(1).Find(&archived).Error; err != nil {
					return err
				}
				if len(archived) > 0 {
					if archived[0].Checksum != checksumText {
						return fmt.Errorf("legacy archive mismatch for %s row %s", source.table, row.SourcePrimaryKey)
					}
					// Verify the persisted JSON itself, not only its accompanying hash.
					var canonical string
					if err := transaction.Raw("SELECT original_row_json::text FROM lobby_legacy_records WHERE id = ?", archived[0].ID).Scan(&canonical).Error; err != nil {
						return err
					}
					if canonical != row.OriginalRowJSON {
						return fmt.Errorf("legacy archive content mismatch for %s row %s", source.table, row.SourcePrimaryKey)
					}
				} else {
					var timestamps struct {
						CreatedAt *time.Time `json:"created_at"`
						UpdatedAt *time.Time `json:"updated_at"`
						DeletedAt *time.Time `json:"deleted_at"`
					}
					if err := json.Unmarshal([]byte(row.OriginalRowJSON), &timestamps); err != nil {
						return err
					}
					identifier, err := uuid.NewRandom()
					if err != nil {
						return err
					}
					record := lobbypostgres.LegacyRecord{ID: identifier.String(), SourceTable: source.table, SourcePrimaryKey: row.SourcePrimaryKey,
						OriginalRowJSON: datatypes.JSON(row.OriginalRowJSON), Checksum: checksumText, SourceCreatedAt: timestamps.CreatedAt,
						SourceUpdatedAt: timestamps.UpdatedAt, SourceDeletedAt: timestamps.DeletedAt, ArchivedAt: time.Now().UTC(), MigrationVersion: legacyLobbyVersion}
					if err := transaction.Create(&record).Error; err != nil {
						return err
					}
				}
				fmt.Fprintf(digest, "%s:%s:%s\n", source.table, row.SourcePrimaryKey, checksumText)
				if source.table == "players" {
					report.Players++
				} else {
					report.Games++
				}
			}
		}
		var archivedCount int64
		if err := transaction.Model(&lobbypostgres.LegacyRecord{}).Where(&lobbypostgres.LegacyRecord{MigrationVersion: legacyLobbyVersion}).Count(&archivedCount).Error; err != nil {
			return err
		}
		if archivedCount != report.Players+report.Games {
			return fmt.Errorf("legacy archive count mismatch")
		}
		if transaction.Migrator().HasTable("games") {
			if err := transaction.Exec("ALTER TABLE games ADD COLUMN IF NOT EXISTS lobby_id uuid").Error; err != nil {
				return err
			}
			if err := transaction.Exec("CREATE UNIQUE INDEX IF NOT EXISTS idx_games_lobby_id ON games(lobby_id)").Error; err != nil {
				return err
			}
			if transaction.Migrator().HasColumn("games", "room_name") {
				if err := transaction.Exec("ALTER TABLE games ALTER COLUMN room_name DROP NOT NULL, ALTER COLUMN room_name DROP DEFAULT").Error; err != nil {
					return err
				}
			}
		}
		report.Checksum = hex.EncodeToString(digest.Sum(nil))
		return transaction.Create(&migrationRecord{Version: legacyLobbyVersion, Players: report.Players, Games: report.Games, Checksum: report.Checksum, AppliedAt: time.Now().UTC()}).Error
	})
	return report, err
}

// VerifyArchive checks persisted archive bytes against per-row digests after
// cutover. It does not import or claim rows, or compare with post-cutover sources.
func VerifyArchive(ctx context.Context, database *gorm.DB) error {
	return database.WithContext(ctx).Transaction(func(transaction *gorm.DB) error {
		var markers []migrationRecord
		if err := transaction.Where(&migrationRecord{Version: legacyLobbyVersion}).Limit(1).Find(&markers).Error; err != nil {
			return err
		}
		if len(markers) == 0 {
			return fmt.Errorf("legacy migration is not complete")
		}
		var records []lobbypostgres.LegacyRecord
		if err := transaction.Where(&lobbypostgres.LegacyRecord{MigrationVersion: legacyLobbyVersion}).Find(&records).Error; err != nil {
			return err
		}
		sort.Slice(records, func(left, right int) bool {
			if records[left].SourceTable != records[right].SourceTable {
				return records[left].SourceTable > records[right].SourceTable
			}
			leftID, _ := strconv.ParseUint(records[left].SourcePrimaryKey, 10, 64)
			rightID, _ := strconv.ParseUint(records[right].SourcePrimaryKey, 10, 64)
			return leftID < rightID
		})
		digest := sha256.New()
		var players, games int64
		for _, record := range records {
			if record.SourceTable != "players" && record.SourceTable != "games" {
				return fmt.Errorf("unexpected archive source")
			}
			if _, err := strconv.ParseUint(record.SourcePrimaryKey, 10, 64); err != nil {
				return fmt.Errorf("invalid archive source key")
			}
			var canonical string
			if err := transaction.Raw("SELECT original_row_json::text FROM lobby_legacy_records WHERE id = ?", record.ID).Scan(&canonical).Error; err != nil {
				return err
			}
			rowDigest := sha256.Sum256([]byte(canonical))
			if hex.EncodeToString(rowDigest[:]) != record.Checksum {
				return fmt.Errorf("archive verification failed for %s row %s", record.SourceTable, record.SourcePrimaryKey)
			}
			fmt.Fprintf(digest, "%s:%s:%s\n", record.SourceTable, record.SourcePrimaryKey, record.Checksum)
			if record.SourceTable == "players" {
				players++
			} else {
				games++
			}
		}
		if players != markers[0].Players || games != markers[0].Games || hex.EncodeToString(digest.Sum(nil)) != markers[0].Checksum {
			return fmt.Errorf("archive verification count or set checksum mismatch")
		}
		return nil
	})
}
