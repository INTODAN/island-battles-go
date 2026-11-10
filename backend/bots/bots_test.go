package bots_test

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"islandBattles/backend/bots"
	"islandBattles/backend/models"

	islandbattles "github.com/INTODAN/island-battles-go/clients/island-battles-go"
	"github.com/INTODAN/island-battles-go/clients/island-battles-go/examples/defaultbots"
)

func TestDefaultsListLevelsOneToThree(t *testing.T) {
	catalog := bots.Defaults()
	if len(catalog) != 3 {
		t.Fatalf("got %d default bots, want 3", len(catalog))
	}
	for index, bot := range catalog {
		if bot.Level != uint(index+1) || bot.Name == "" || bot.Description == "" {
			t.Fatalf("unexpected catalog entry %+v", bot)
		}
		if _, found := bots.ForLevel(bot.Level); !found {
			t.Fatalf("level %d has no bot", bot.Level)
		}
	}
	if _, found := bots.ForLevel(4); found {
		t.Fatal("level 4 has a bot")
	}
}

// fullTurn is a BotTurn with every field set, and every kind of entity.
func fullTurn() models.BotTurn {
	deadline := time.Date(2026, 11, 7, 12, 0, 30, 123456789, time.UTC)
	upgrades := models.Upgrades{models.SpeedUpgrade: 2, models.ClashStopGenerationsUpgrade: 3}
	return models.BotTurn{
		GameID:    7,
		TeamColor: 2,
		Upgrades:  models.UpgradeTable(),
		Memory:    models.BotMemory(`{"plans":[{"target":3,"at":1e3}],"name":"é","big":123456789012345678901234567890}`),
		Generation: models.GenerationView{
			ID: 41,
			Castles: []models.CastleView{
				{ID: 0, TeamColor: 2, Soldiers: 31, X: -3.25, Y: 0.1, Upgrades: upgrades},
				{ID: 4, TeamColor: models.NeutralTeamColor, Soldiers: 10, X: 0, Y: 2, Upgrades: models.Upgrades{}.Clone()},
			},
			SoldierGroups: []models.SoldierGroupView{
				{ID: 9, TeamColor: 1, Soldiers: 4, X: 1.5, Y: -0.75, OriginCastleID: 4, DestinationCastleID: 0, ClashRadius: 0.25, Upgrades: upgrades},
			},
			Clashes: []models.Clash{{ID: 1, Time: 0.5, Participants: []models.ClashParticipant{
				{GroupID: 3, TeamColor: 1, X: 1, Y: 2, ClashRadius: 0.25, SoldiersBefore: 8, SoldiersAfter: 0},
			}}},
		},
		TurnDeadlineAt: &deadline,
	}
}

// fullResult is a BotTurnResult with both command types, log lines and memory.
func fullResult() models.BotTurnResult {
	return models.BotTurnResult{
		Commands: []models.PublicGenerationCommand{
			{CommandType: models.SendSoldierGroup, Parameters: json.RawMessage(`{"origin_castle_id":0,"destination_castle_id":4,"soldier_amount":12}`)},
			{CommandType: models.UpgradeCastle, Parameters: json.RawMessage(`{"castle_id":0,"upgrade_type":"speed"}`)},
		},
		Memory: models.BotMemory(`{"turn":3}`),
		Logs:   []models.GenerationLogLine{{Level: models.GenerationLogWarning, Logger: "island_battles.player", Message: "attacking castle 4"}},
	}
}

// roundTrip sends value through JSON into the SDK's type and back.
func roundTrip[Server, SDK any](t *testing.T, value Server) Server {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var sdkValue SDK
	if err := json.Unmarshal(encoded, &sdkValue); err != nil {
		t.Fatal(err)
	}
	if encoded, err = json.Marshal(sdkValue); err != nil {
		t.Fatal(err)
	}
	var back Server
	if err := json.Unmarshal(encoded, &back); err != nil {
		t.Fatal(err)
	}
	return back
}

// The SDK's Turn and Result carry everything of the server's BotTurn and
// BotTurnResult, which the default bots' adapter relies on.
func TestTurnAndResultSurviveTheSDKTypes(t *testing.T) {
	turn := fullTurn()
	if back := roundTrip[models.BotTurn, islandbattles.Turn](t, turn); !reflect.DeepEqual(back, turn) {
		t.Fatalf("the turn changed:\n%+v\n%+v", turn, back)
	}
	turn.Memory, turn.TurnDeadlineAt = nil, nil
	if back := roundTrip[models.BotTurn, islandbattles.Turn](t, turn); !reflect.DeepEqual(back, turn) {
		t.Fatalf("the turn without memory or deadline changed:\n%+v\n%+v", turn, back)
	}
	result := fullResult()
	if back := roundTrip[models.BotTurnResult, islandbattles.Result](t, result); !reflect.DeepEqual(back, result) {
		t.Fatalf("the result changed:\n%+v\n%+v", result, back)
	}
	empty := models.BotTurnResult{Commands: []models.PublicGenerationCommand{}, Logs: []models.GenerationLogLine{}}
	if back := roundTrip[models.BotTurnResult, islandbattles.Result](t, empty); !reflect.DeepEqual(back, empty) {
		t.Fatalf("the empty result changed:\n%+v\n%+v", empty, back)
	}
}

// The SDK's fixture turn, which its own tests and the default bots' tests
// play, has the server's upgrade table.
func TestTheSDKFixtureHasTheServersUpgradeTable(t *testing.T) {
	var table []islandbattles.Upgrade
	encoded, _ := json.Marshal(models.UpgradeTable())
	if err := json.Unmarshal(encoded, &table); err != nil {
		t.Fatal(err)
	}
	if fixture := islandbattles.FixtureTurn().Upgrades; !reflect.DeepEqual(fixture, table) {
		t.Fatalf("the fixture's table is %+v, the server's %+v", fixture, table)
	}
}

// Each level plays its SDK example, on the server's types.
func TestLevelsPlayTheSDKExamples(t *testing.T) {
	examples := map[uint]islandbattles.Play{1: defaultbots.LevelOne, 2: defaultbots.LevelTwo, 3: defaultbots.LevelThree}
	turn := fullTurn()
	turn.Memory = nil
	var sdkTurn islandbattles.Turn
	encoded, _ := json.Marshal(turn)
	if err := json.Unmarshal(encoded, &sdkTurn); err != nil {
		t.Fatal(err)
	}
	for level, example := range examples {
		bot, _ := bots.ForLevel(level)
		result, err := bot.Play(context.Background(), turn)
		if err != nil {
			t.Fatal(err)
		}
		want, err := example(context.Background(), sdkTurn)
		if err != nil {
			t.Fatal(err)
		}
		var got islandbattles.Result
		encoded, _ := json.Marshal(result)
		if err := json.Unmarshal(encoded, &got); err != nil {
			t.Fatal(err)
		}
		wantEncoded, _ := json.Marshal(want)
		if gotEncoded, _ := json.Marshal(got); string(gotEncoded) != string(wantEncoded) {
			t.Fatalf("level %d played %s, its example %s", level, gotEncoded, wantEncoded)
		}
		if len(result.Logs) == 0 || result.Logs[0].Logger != "island_battles.bots.level"+string(rune('0'+level)) {
			t.Fatalf("level %d logged %+v", level, result.Logs)
		}
	}
}
