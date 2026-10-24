package dal_test

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"islandBattles/backend/dal"
	"islandBattles/backend/internal/testutils"
	"islandBattles/backend/models"
	"islandBattles/backend/services"
	"islandBattles/backend/simulation"

	"github.com/jackc/pgx/v5/pgtype"
	"gorm.io/gorm"
)

func TestGameStateRoundTrip(t *testing.T) {
	database := testutils.InitTestDB(t)
	generationID := testutils.CreateComplexGenerationForTest(t, database)
	generation, err := dal.GetGeneration(database, generationID)
	if err != nil {
		t.Fatal(err)
	}
	castle, err := dal.GetCastleFromGeneration(generation, 2)
	if err != nil {
		t.Fatal(err)
	}
	castle.Upgrades[models.SpeedUpgrade] = 3
	if err := database.Save(castle).Error; err != nil {
		t.Fatal(err)
	}
	origin, err := dal.GetCastleFromGeneration(generation, 1)
	if err != nil {
		t.Fatal(err)
	}
	group := models.SoldierGroup{
		SoldierGroupFields: models.SoldierGroupFields{OriginCastleID: origin.ID, DestinationCastleID: castle.ID, SoldierAmount: 7},
		GenerationID:       generationID, TeamColor: 2, MovementPauseGenerations: 2, ClashRadius: .75,
		Position: pgtype.Point{P: pgtype.Vec2{X: 3.5, Y: 1.25}, Valid: true}, Upgrades: models.Upgrades{models.ClashStopGenerationsUpgrade: 2},
	}
	testutils.CreateSoldierGroupForTest(t, database, &group)

	state, err := dal.LoadGameState(database, generation.GameID)
	if err != nil {
		t.Fatal(err)
	}
	if state.Map != generation.Map.MapFields || state.NextSoldierGroupID != 1 || len(state.Castles) != 3 || len(state.SoldierGroups) != 1 {
		t.Fatalf("loaded state: %+v", state)
	}
	if loaded := state.Castles[2]; loaded.ID != 2 || loaded.TeamColor != 2 || loaded.Soldiers != 15 ||
		loaded.Position != castle.Position || loaded.Upgrades[models.SpeedUpgrade] != 3 {
		t.Fatalf("loaded castle: %+v", loaded)
	}
	expectedGroup := simulation.SoldierGroup{
		ID: 0, TeamColor: 2, Soldiers: 7, Position: group.Position, OriginCastleID: 1, DestinationCastleID: 2,
		MovementPauseGenerations: 2, ClashRadius: .75, Upgrades: models.Upgrades{models.SpeedUpgrade: 1, models.ClashStopGenerationsUpgrade: 2},
	}
	if !reflect.DeepEqual(*state.SoldierGroups[0], expectedGroup) {
		t.Fatalf("loaded group: %+v", state.SoldierGroups[0])
	}

	state.Castles[0].TeamColor, state.Castles[0].Soldiers = 2, 4
	state.SoldierGroups[5] = &simulation.SoldierGroup{
		ID: 5, TeamColor: 1, Soldiers: 3, Position: state.Castles[0].Position, OriginCastleID: 0, DestinationCastleID: 1,
		ClashRadius: models.DefaultClashRadius, Upgrades: models.Upgrades{}.Clone(),
	}
	state.NextSoldierGroupID = 6
	commands := append([]models.CommandFields{{CommandType: models.UpgradeCastle, Parameters: []byte(`{"castle_id":2,"upgrade_type":"speed"}`)}}, simulation.EndOfTurnCommands()...)
	clashes := []models.Clash{{ID: 1, Time: .5, Participants: []models.ClashParticipant{{GroupID: 3, TeamColor: 1, X: 1, Y: 2, ClashRadius: .25, SoldiersBefore: 4}}}}
	savedID, err := dal.SaveGeneration(database, generation.GameID, state, commands, clashes)
	if err != nil {
		t.Fatal(err)
	}

	reloaded, err := dal.LoadGameState(database, generation.GameID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(reloaded, state) {
		t.Fatalf("state changed across save and load:\n%+v\n%+v", reloaded, state)
	}
	saved, err := dal.GetGeneration(database, savedID)
	if err != nil {
		t.Fatal(err)
	}
	if saved.GameID != generation.GameID || saved.MapID == generation.MapID || !reflect.DeepEqual(saved.Clashes, clashes) {
		t.Fatalf("saved generation: %+v", saved)
	}
	if len(saved.Commands) != len(commands) {
		t.Fatalf("saved commands: %+v", saved.Commands)
	}
	for index, command := range saved.Commands {
		if command.CommandType != commands[index].CommandType {
			t.Fatalf("command %d: %+v", index, command)
		}
	}
	for _, savedGroup := range saved.SoldierGroups {
		if savedGroup.OriginCastle.MapID != saved.MapID || savedGroup.DestinationCastle.MapID != saved.MapID {
			t.Fatalf("group %d points at another generation's castles", savedGroup.PersistentSoldierGroupID)
		}
	}
	var game models.Game
	if err := database.First(&game, generation.GameID).Error; err != nil {
		t.Fatal(err)
	}
	if game.NextSoldierGroupID != 6 {
		t.Fatalf("next soldier group ID = %d", game.NextSoldierGroupID)
	}
	previous, err := dal.GetGeneration(database, generationID)
	if err != nil {
		t.Fatal(err)
	}
	if previousCastle, _ := dal.GetCastleFromGeneration(previous, 0); previousCastle.TeamColor != 1 || len(previous.SoldierGroups) != 1 {
		t.Fatal("saving changed the previous generation")
	}
}

func TestLoadGameStateOfMissingGame(t *testing.T) {
	database := testutils.InitTestDB(t)
	if _, err := dal.LoadGameState(database, 99); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("missing game: %v", err)
	}
	gameID, err := dal.CreateGame(database)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := dal.LoadGameState(database, gameID); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("game without generations: %v", err)
	}
}

// A turn reads the database a fixed number of times and writes each table once,
// however many groups and commands it has.
func TestTurnStatementsDoNotGrowWithGroupsOrCommands(t *testing.T) {
	database := testutils.InitTestDB(t)
	parameters, err := json.Marshal(simulation.SendSoldierGroupParams{OriginPersistentCastleID: 0, DestinationPersistentCastleID: 1, SoldierAmount: 1})
	if err != nil {
		t.Fatal(err)
	}
	send := models.CommandFields{CommandType: models.SendSoldierGroup, Parameters: parameters}
	statements := func(commands []models.CommandFields) map[string]int {
		generation, err := dal.GetGeneration(database, testutils.CreateGenerationForTest(t, database))
		if err != nil {
			t.Fatal(err)
		}
		// Groups already on the road are carried over as well as new ones.
		if _, err := services.MigrateGeneration(database, generation.GameID, commands); err != nil {
			t.Fatal(err)
		}
		counts := map[string]int{}
		count := func(kind string) func(*gorm.DB) {
			return func(statement *gorm.DB) { counts[kind+" "+statement.Statement.Table]++ }
		}
		const name = "test:count-turn-statements"
		callbacks := database.Callback()
		if err := callbacks.Query().After("gorm:query").Register(name, count("select")); err != nil {
			t.Fatal(err)
		}
		if err := callbacks.Create().After("gorm:create").Register(name, count("insert")); err != nil {
			t.Fatal(err)
		}
		if err := callbacks.Update().After("gorm:update").Register(name, count("update")); err != nil {
			t.Fatal(err)
		}
		if err := callbacks.Delete().After("gorm:delete").Register(name, count("delete")); err != nil {
			t.Fatal(err)
		}
		_, err = services.MigrateGeneration(database, generation.GameID, commands)
		for _, remove := range []func(string) error{callbacks.Query().Remove, callbacks.Create().Remove, callbacks.Update().Remove, callbacks.Delete().Remove} {
			if removeErr := remove(name); removeErr != nil {
				t.Fatal(removeErr)
			}
		}
		if err != nil {
			t.Fatal(err)
		}
		return counts
	}
	small := statements([]models.CommandFields{send})
	large := statements([]models.CommandFields{send, send, send, send, send, send, send, send})
	if !reflect.DeepEqual(small, large) {
		t.Fatalf("statements grew with the turn's size:\nsmall: %v\nlarge: %v", small, large)
	}
	t.Logf("statements per turn: %v", small)
	if small["select generations"] != 1 {
		t.Fatalf("expected one generation load: %v", small)
	}
}
