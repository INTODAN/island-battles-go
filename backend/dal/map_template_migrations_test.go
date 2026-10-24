package dal_test

import (
	"context"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"
	"testing/fstest"

	"islandBattles/backend/dal"
	"islandBattles/backend/internal/testutils"
	"islandBattles/backend/models"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type mapTemplateHistoryRow struct {
	Version int64
	Name    string
	Adopted bool
}

func TestMapTemplateMigrationsFreshInstall(t *testing.T) {
	// No AutoMigrate runs here, so model fields the migrations lack show up.
	db := testutils.InitEmptyTestDB(t)
	if err := dal.MigrateMapTemplates(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	history := mapTemplateHistory(t, db)
	names := mapTemplateMigrationNames(t, dal.MapTemplateMigrations().Files)
	if len(history) != len(names) {
		t.Fatalf("history = %+v, want versions for %v", history, names)
	}
	for index, row := range history {
		if row.Version != int64(index+1) || row.Name != names[index] || row.Adopted {
			t.Fatalf("history[%d] = %+v, want version %d %s run, not adopted", index, row, index+1, names[index])
		}
	}
	for _, record := range []any{&models.MapTemplate{}, &models.CastleTemplate{}} {
		assertRecordMatchesColumns(t, db, record)
	}
}

func TestMapTemplateMigrationsAdoptSchemaCreatedBeforeVersioning(t *testing.T) {
	legacy := legacyMapTemplates(t)
	templateID := insertLegacyMapTemplate(t, legacy)

	// Migration 1 has no IF NOT EXISTS clauses, so running it here would fail.
	if err := dal.MigrateMapTemplates(context.Background(), legacy); err != nil {
		t.Fatalf("migrate legacy map templates: %v", err)
	}
	history := mapTemplateHistory(t, legacy)
	if len(history) == 0 || history[0].Version != 1 || !history[0].Adopted {
		t.Fatalf("history = %+v, want version 1 adopted", history)
	}
	var saved models.MapTemplate
	if err := legacy.Preload("CastleTemplates").First(&saved, templateID).Error; err != nil {
		t.Fatal(err)
	}
	if len(saved.CastleTemplates) != 2 {
		t.Fatalf("map template after adoption = %#v, want its 2 castles", saved)
	}
	// Castles stored before starting_soldier_amount existed start with the
	// template's neutral count, and the adopted tables take the new column.
	assertTemplateStartsWith(t, legacy, templateID, map[uint]uint{0: 15, 1: 15})
	assertTemplateStartsWith(t, legacy, createExampleMapTemplate(t, legacy, 30), map[uint]uint{0: 30, 1: 15})

	fresh := testutils.InitEmptyTestDB(t)
	if err := dal.MigrateMapTemplates(context.Background(), fresh); err != nil {
		t.Fatal(err)
	}
	got, want := mapTemplateSchema(t, legacy), mapTemplateSchema(t, fresh)
	if !slices.Equal(got, want) {
		t.Fatalf("adopted schema = %v\nfresh schema = %v", got, want)
	}
}

// A database that GORM AutoMigrate gave starting_soldier_amount before
// migration 2 existed keeps the column and its counts.
func TestMapTemplateMigrationsAdoptSchemaWithStartingSoldiers(t *testing.T) {
	legacy := legacyMapTemplates(t, `ALTER TABLE castle_templates ADD COLUMN starting_soldier_amount bigint`)
	templateID := insertLegacyMapTemplate(t, legacy)
	if err := legacy.Exec(`UPDATE castle_templates SET starting_soldier_amount = 40 WHERE persistent_castle_id = 0`).Error; err != nil {
		t.Fatal(err)
	}
	if err := dal.MigrateMapTemplates(context.Background(), legacy); err != nil {
		t.Fatalf("migrate legacy map templates: %v", err)
	}
	if history := mapTemplateHistory(t, legacy); len(history) != len(mapTemplateMigrationNames(t, dal.MapTemplateMigrations().Files)) || !history[0].Adopted || history[1].Adopted {
		t.Fatalf("history = %+v, want version 1 adopted and the rest run", history)
	}
	assertTemplateStartsWith(t, legacy, templateID, map[uint]uint{0: 40, 1: 15})

	fresh := testutils.InitEmptyTestDB(t)
	if err := dal.MigrateMapTemplates(context.Background(), fresh); err != nil {
		t.Fatal(err)
	}
	if got, want := mapTemplateSchema(t, legacy), mapTemplateSchema(t, fresh); !slices.Equal(got, want) {
		t.Fatalf("adopted schema = %v\nfresh schema = %v", got, want)
	}
}

func TestMapTemplateMigrationsRefuseToAdoptPartialSchema(t *testing.T) {
	// GORM's AutoMigrate wasn't atomic, so it could leave only map_templates.
	legacy := legacyMapTemplates(t, `DROP TABLE castle_templates`)

	err := dal.MigrateMapTemplates(context.Background(), legacy)
	if err == nil || !strings.HasPrefix(err.Error(), dal.MapTemplateMigrations().HistoryTable+": ") ||
		!strings.Contains(err.Error(), "has map_templates but lacks castle_templates") {
		t.Fatalf("migrate partial schema error = %v, want castle_templates named", err)
	}
	if legacy.Migrator().HasTable("castle_templates") || legacy.Migrator().HasTable(dal.MapTemplateMigrations().HistoryTable) {
		t.Fatal("a refused adoption changed the database")
	}
}

func TestMapTemplateMigrationsRefuseToAdoptOutdatedSchema(t *testing.T) {
	// Older AutoMigrate runs had neither column.
	legacy := legacyMapTemplates(t, `ALTER TABLE castle_templates DROP COLUMN persistent_castle_id, DROP COLUMN upgrades`)

	err := dal.MigrateMapTemplates(context.Background(), legacy)
	if err == nil || !strings.Contains(err.Error(), "lacks castle_templates.persistent_castle_id, castle_templates.upgrades,") {
		t.Fatalf("migrate outdated schema error = %v, want the missing columns named", err)
	}
	if legacy.Migrator().HasTable(dal.MapTemplateMigrations().HistoryTable) {
		t.Fatal("a refused adoption recorded history")
	}
}

func TestMapTemplateMigrationsApplyNewMigrationToExistingDatabase(t *testing.T) {
	db := testutils.InitTestDB(t)
	templateID := createExampleMapTemplate(t, db, 15)

	set := dal.MapTemplateMigrations()
	names := mapTemplateMigrationNames(t, set.Files)
	next := fmt.Sprintf("%04d_require_players.sql", len(names)+1)
	files := fstest.MapFS{next: &fstest.MapFile{Data: []byte(`ALTER TABLE map_templates ADD CONSTRAINT ck_map_templates_player_amount CHECK (player_amount > 0);`)}}
	for _, name := range names {
		data, err := fs.ReadFile(set.Files, name)
		if err != nil {
			t.Fatal(err)
		}
		files[name] = &fstest.MapFile{Data: data}
	}
	set.Files = files
	if err := set.Apply(context.Background(), db); err != nil {
		t.Fatalf("apply new migration: %v", err)
	}

	if err := db.Create(&models.MapTemplate{MapFields: models.MapFields{MapName: "no-players"}}).Error; err == nil {
		t.Fatal("new constraint was not applied")
	}
	if history := mapTemplateHistory(t, db); history[len(history)-1].Name != next {
		t.Fatalf("history = %+v, want %s last", history, next)
	}
	var preserved models.MapTemplate
	if err := db.First(&preserved, templateID).Error; err != nil {
		t.Fatalf("existing map template after upgrade: %v", err)
	}
}

func TestConcurrentMapTemplateMigrationsOfEmptyDatabaseAllSucceed(t *testing.T) {
	db := testutils.InitEmptyTestDB(t)
	const runs = 4
	start := make(chan struct{})
	errs := make(chan error, runs)
	var group sync.WaitGroup
	for range runs {
		group.Go(func() {
			<-start
			errs <- dal.MigrateMapTemplates(context.Background(), db)
		})
	}
	close(start)
	group.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Errorf("concurrent migration: %v", err)
		}
	}
	if history, names := mapTemplateHistory(t, db), mapTemplateMigrationNames(t, dal.MapTemplateMigrations().Files); len(history) != len(names) {
		t.Fatalf("history = %+v, want one row for each of %v", history, names)
	}
}

// legacyMapTemplates returns a database with the map template tables as they
// were before versioning, changed by the given statements.
func legacyMapTemplates(t *testing.T, changes ...string) *gorm.DB {
	t.Helper()
	legacy := testutils.InitEmptyTestDB(t)
	content, err := os.ReadFile("testdata/legacy_map_templates.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range append([]string{string(content)}, changes...) {
		if err := legacy.Exec(statement).Error; err != nil {
			t.Fatalf("create legacy map templates: %v", err)
		}
	}
	return legacy
}

// createExampleMapTemplate stores the example map file, starting its first
// castle with firstCastleSoldiers. Every count gets its own map name.
func createExampleMapTemplate(t *testing.T, db *gorm.DB, firstCastleSoldiers uint) uint {
	t.Helper()
	mapTemplate, err := models.DecodeMapTemplate([]byte(testutils.MapExampleInput))
	if err != nil {
		t.Fatal(err)
	}
	mapTemplate.MapName = fmt.Sprintf("%s-%d", mapTemplate.MapName, firstCastleSoldiers)
	mapTemplate.Castles[0].StartingSoldierAmount = &firstCastleSoldiers
	id, err := dal.CreateMapTemplate(db, mapTemplate)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

// insertLegacyMapTemplate stores a two-castle template the way code from
// before versioning did, without the columns that later migrations add.
func insertLegacyMapTemplate(t *testing.T, db *gorm.DB) uint {
	t.Helper()
	var templateID uint
	if err := db.Raw(`INSERT INTO map_templates (created_at, updated_at, map_name, player_amount, plane_size_y, plane_size_x, neutral_starting_soldier_amount)
VALUES (now(), now(), 'legacy', 2, 6, 8, 15) RETURNING id`).Scan(&templateID).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`INSERT INTO castle_templates (created_at, updated_at, team_color, "position", persistent_castle_id, upgrades, map_template_id)
VALUES (now(), now(), 1, point(-3, 0), 0, '{}', ?), (now(), now(), 2, point(3, 0), 1, '{}', ?)`, templateID, templateID).Error; err != nil {
		t.Fatal(err)
	}
	return templateID
}

// assertTemplateStartsWith builds a game map from a stored template and
// compares each castle's starting soldiers by castle ID.
func assertTemplateStartsWith(t *testing.T, db *gorm.DB, templateID uint, want map[uint]uint) {
	t.Helper()
	var template models.MapTemplate
	if err := db.Preload(clause.Associations).First(&template, templateID).Error; err != nil {
		t.Fatal(err)
	}
	got := map[uint]uint{}
	for _, castle := range models.BuildMap(template.View()).Castles {
		got[castle.PersistentCastleID] = castle.SoldierAmount
	}
	if !maps.Equal(got, want) {
		t.Fatalf("template %d starting soldiers = %v, want %v", templateID, got, want)
	}
}

func mapTemplateHistory(t *testing.T, db *gorm.DB) []mapTemplateHistoryRow {
	t.Helper()
	var rows []mapTemplateHistoryRow
	if err := db.Table(dal.MapTemplateMigrations().HistoryTable).Order(clause.OrderByColumn{Column: clause.Column{Name: "version"}}).Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	return rows
}

func mapTemplateMigrationNames(t *testing.T, files fs.FS) []string {
	t.Helper()
	entries, err := fs.ReadDir(files, ".")
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	return names
}

func assertRecordMatchesColumns(t *testing.T, db *gorm.DB, record any) {
	t.Helper()
	statement := &gorm.Statement{DB: db}
	if err := statement.Parse(record); err != nil {
		t.Fatal(err)
	}
	var fields []string
	for _, field := range statement.Schema.Fields {
		if field.DBName != "" {
			fields = append(fields, field.DBName)
		}
	}
	columnTypes, err := db.Migrator().ColumnTypes(record)
	if err != nil {
		t.Fatal(err)
	}
	var columns []string
	for _, columnType := range columnTypes {
		columns = append(columns, columnType.Name())
	}
	slices.Sort(fields)
	slices.Sort(columns)
	if !slices.Equal(fields, columns) {
		t.Errorf("%s columns = %v, model fields = %v; add a migration", statement.Schema.Table, columns, fields)
	}
}

// mapTemplateSchema lists the template tables' columns, constraints and
// indexes. Column order is ignored.
func mapTemplateSchema(t *testing.T, db *gorm.DB) []string {
	t.Helper()
	tables := []string{"map_templates", "castle_templates"}
	var lines []string
	err := db.Raw(`
SELECT format('column %s.%s %s nullable=%s default=%s', table_name, column_name, data_type, is_nullable, coalesce(column_default, ''))
FROM information_schema.columns
WHERE table_schema = current_schema() AND table_name IN ?
UNION ALL
SELECT format('constraint %s.%s %s', conrelid::regclass, conname, pg_get_constraintdef(oid))
FROM pg_constraint
WHERE connamespace = current_schema()::regnamespace AND conrelid::regclass::text IN ?
UNION ALL
SELECT format('index %s.%s %s', tablename, indexname, indexdef)
FROM pg_indexes
WHERE schemaname = current_schema() AND tablename IN ?
ORDER BY 1`, tables, tables, tables).Scan(&lines).Error
	if err != nil {
		t.Fatal(err)
	}
	return lines
}
