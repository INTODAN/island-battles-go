package bots_test

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"islandBattles/backend/bots"
	"islandBattles/backend/models"
)

// castle is a castle of a fixture generation, with level 1 upgrades.
func castle(id, team, soldiers uint, x, y float64) models.CastleView {
	return models.CastleView{ID: id, TeamColor: team, Soldiers: soldiers, X: x, Y: y, Upgrades: models.Upgrades{}.Clone()}
}

// group is a soldier group of a fixture generation, with level 1 upgrades.
func group(id, team, soldiers uint, x, y float64, origin, destination uint) models.SoldierGroupView {
	return models.SoldierGroupView{ID: id, TeamColor: team, Soldiers: soldiers, X: x, Y: y, OriginCastleID: origin, DestinationCastleID: destination,
		ClashRadius: models.DefaultClashRadius, Upgrades: models.Upgrades{}.Clone()}
}

func turn(team uint, castles []models.CastleView, groups []models.SoldierGroupView, memory string) models.BotTurn {
	if groups == nil {
		groups = []models.SoldierGroupView{}
	}
	botTurn := models.BotTurn{GameID: 7, TeamColor: team, Upgrades: models.UpgradeTable(), Generation: models.GenerationView{ID: 41, Castles: castles, SoldierGroups: groups, Clashes: []models.Clash{}}}
	if memory != "" {
		botTurn.Memory = models.BotMemory(memory)
	}
	return botTurn
}

// sent is a send_soldier_group command's parameters.
type sent struct {
	Origin      uint `json:"origin_castle_id"`
	Destination uint `json:"destination_castle_id"`
	Soldiers    uint `json:"soldier_amount"`
}

// play plays a turn with the level's bot, checks what every default bot's
// result must be, and returns it with its sends and upgrades.
func play(t *testing.T, level uint, botTurn models.BotTurn) (models.BotTurnResult, []sent, []models.UpgradeCastleParameters) {
	t.Helper()
	bot, found := bots.ForLevel(level)
	if !found {
		t.Fatalf("no bot of level %d", level)
	}
	result, err := bot.Play(context.Background(), botTurn)
	if err != nil {
		t.Fatal(err)
	}
	again, err := bot.Play(context.Background(), botTurn)
	if err != nil || !reflect.DeepEqual(result, again) {
		t.Fatalf("the same turn gave different results:\n%+v\n%+v", result, again)
	}
	if result.Commands == nil {
		t.Fatal("commands are null")
	}
	if len(result.Logs) == 0 || len(result.Logs) > 2 {
		t.Fatalf("got %d log lines, want 1 or 2: %+v", len(result.Logs), result.Logs)
	}
	for _, line := range result.Logs {
		if line.Level != models.GenerationLogInfo || line.Message == "" {
			t.Fatalf("unexpected log line %+v", line)
		}
	}
	sends, upgrades := []sent{}, []models.UpgradeCastleParameters{}
	for _, command := range result.Commands {
		switch command.CommandType {
		case models.SendSoldierGroup:
			var parameters sent
			if err := json.Unmarshal(command.Parameters, &parameters); err != nil {
				t.Fatal(err)
			}
			sends = append(sends, parameters)
		case models.UpgradeCastle:
			var parameters models.UpgradeCastleParameters
			if err := json.Unmarshal(command.Parameters, &parameters); err != nil {
				t.Fatal(err)
			}
			upgrades = append(upgrades, parameters)
		default:
			t.Fatalf("unexpected command %s", command.CommandType)
		}
	}
	return result, sends, upgrades
}

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

func TestLevelOneSendsOneSoldierToTheFirstEnemyCastle(t *testing.T) {
	castles := []models.CastleView{castle(0, 1, 0, 0, 0), castle(1, 1, 5, 1, 0), castle(2, 0, 3, 2, 0), castle(3, 2, 9, 3, 0), castle(4, 2, 9, 4, 0)}
	result, sends, _ := play(t, 1, turn(1, castles, nil, ""))
	if !reflect.DeepEqual(sends, []sent{{Origin: 1, Destination: 3, Soldiers: 1}}) || result.Memory != nil {
		t.Fatalf("got sends %+v and memory %s", sends, result.Memory)
	}
	// Neutral castles are never its target.
	_, sends, _ = play(t, 1, turn(1, castles[:3], nil, ""))
	if len(sends) != 0 {
		t.Fatalf("attacked without an enemy castle: %+v", sends)
	}
}

func TestLevelTwoAttacksTheNearestCastleItCanTake(t *testing.T) {
	// Castle 2 is nearest but too strong. Castle 1 lands after 2 turns with
	// 12 defenders, so 12 + 1 + a margin of 3 takes it.
	castles := []models.CastleView{castle(0, 1, 20, 0, 0), castle(1, 0, 10, 1, 0), castle(2, 0, 30, 0, 0.5), castle(3, 2, 30, 6, 0)}
	_, sends, _ := play(t, 2, turn(1, castles, nil, ""))
	if !reflect.DeepEqual(sends, []sent{{Origin: 0, Destination: 1, Soldiers: 16}}) {
		t.Fatalf("got sends %+v", sends)
	}
	// A group of its own already takes castle 1, so it waits rather than
	// attack castle 3, which it can't beat.
	groups := []models.SoldierGroupView{group(5, 1, 20, 0.5, 0, 0, 1)}
	_, sends, _ = play(t, 2, turn(1, castles, groups, ""))
	if len(sends) != 0 {
		t.Fatalf("got sends %+v", sends)
	}
	// An enemy group landing on castle 1 first leaves fewer defenders.
	groups = []models.SoldierGroupView{group(5, 2, 8, 1.5, 0, 3, 1)}
	_, sends, _ = play(t, 2, turn(1, castles, groups, ""))
	if !reflect.DeepEqual(sends, []sent{{Origin: 0, Destination: 1, Soldiers: 7}}) {
		t.Fatalf("got sends %+v", sends)
	}
}

func TestLevelThreeSendsHelpToAThreatenedCastle(t *testing.T) {
	// 20 enemies land on castle 0 in 2 turns, when it has 7 of its own, and
	// castle 1 is 2 turns away.
	castles := []models.CastleView{castle(0, 1, 5, 0, 0), castle(1, 1, 40, 1, 0), castle(2, 2, 10, 10, 0)}
	groups := []models.SoldierGroupView{group(3, 2, 20, -1, 0, 2, 0)}
	_, sends, _ := play(t, 3, turn(1, castles, groups, ""))
	if len(sends) == 0 || sends[0].Origin != 1 || sends[0].Destination != 0 || sends[0].Soldiers < 14 {
		t.Fatalf("got sends %+v", sends)
	}
}

func TestLevelThreeKeepsAGuardAgainstTheEnemy(t *testing.T) {
	// Castle 1 would take 16 soldiers, but castle 0 keeps 34 against the
	// enemy castle 4 turns away.
	castles := []models.CastleView{castle(0, 1, 40, 0, 0), castle(1, 0, 10, -1, 0), castle(2, 2, 45, 2, 0)}
	_, sends, _ := play(t, 3, turn(1, castles, nil, ""))
	if len(sends) != 0 {
		t.Fatalf("got sends %+v", sends)
	}
	// Level 2 attacks anyway.
	_, sends, _ = play(t, 2, turn(1, castles, nil, ""))
	if len(sends) != 1 || sends[0].Destination != 1 {
		t.Fatalf("got sends %+v", sends)
	}
}

func TestLevelThreeCombinesAttacksAndRemembersTheLaterLaunch(t *testing.T) {
	// Castle 2 lands from castle 1 in 4 turns and from castle 0 in 10.
	// Neither can take it alone, but together they land 36 soldiers after
	// 10 turns: castle 0 sends now, and castle 1 sends in turn 7.
	castles := []models.CastleView{castle(0, 1, 20, 0, 0), castle(1, 1, 20, 3, 0), castle(2, 0, 20, 5, 0)}
	result, sends, _ := play(t, 3, turn(1, castles, nil, ""))
	if !reflect.DeepEqual(sends, []sent{{Origin: 0, Destination: 2, Soldiers: 20}}) {
		t.Fatalf("got sends %+v", sends)
	}
	var memory struct {
		Turn  int `json:"turn"`
		Plans []struct {
			Target   uint `json:"target"`
			Lands    int  `json:"lands"`
			Launches []struct {
				Origin   uint `json:"origin"`
				Soldiers uint `json:"soldiers"`
				At       int  `json:"at"`
			} `json:"launches"`
		} `json:"plans"`
	}
	if err := json.Unmarshal(result.Memory, &memory); err != nil {
		t.Fatal(err)
	}
	if memory.Turn != 1 || len(memory.Plans) != 1 || memory.Plans[0].Target != 2 || memory.Plans[0].Lands != 10 || len(memory.Plans[0].Launches) != 1 ||
		memory.Plans[0].Launches[0].Origin != 1 || memory.Plans[0].Launches[0].Soldiers != 16 || memory.Plans[0].Launches[0].At != 7 {
		t.Fatalf("unexpected memory %s", result.Memory)
	}
	// Castle 1 keeps the soldiers until turn 7, then sends them.
	later := []models.CastleView{castle(0, 1, 5, 0, 0), castle(1, 1, 26, 3, 0), castle(2, 0, 26, 5, 0)}
	groups := []models.SoldierGroupView{group(0, 1, 20, 3, 0, 0, 2)}
	waiting := string(result.Memory)
	waiting = waiting[:len(`{"turn":`)] + "5" + waiting[len(`{"turn":1`):]
	_, sends, _ = play(t, 3, turn(1, later, groups, waiting))
	if len(sends) != 0 {
		t.Fatalf("sent before turn 7: %+v", sends)
	}
	due := waiting[:len(`{"turn":`)] + "6" + waiting[len(`{"turn":5`):]
	result, sends, _ = play(t, 3, turn(1, later, groups, due))
	if !reflect.DeepEqual(sends, []sent{{Origin: 1, Destination: 2, Soldiers: 16}}) || string(result.Memory) != `{"turn":7}` {
		t.Fatalf("got sends %+v and memory %s", sends, result.Memory)
	}
}

func TestLevelThreeBuysSpeedWithSpareSoldiers(t *testing.T) {
	castles := []models.CastleView{castle(0, 1, 40, 0, 0), castle(1, 2, 5, 20, 0)}
	castles[1].Soldiers = 500
	// Early on, and while a guard needs the soldiers, it buys nothing.
	_, _, upgrades := play(t, 3, turn(1, castles, nil, `{"turn":30}`))
	if len(upgrades) != 0 {
		t.Fatalf("got upgrades %+v", upgrades)
	}
	castles[1].Soldiers = 5
	_, _, upgrades = play(t, 3, turn(1, castles, nil, ""))
	if len(upgrades) != 0 {
		t.Fatalf("got upgrades %+v", upgrades)
	}
	_, sends, upgrades := play(t, 3, turn(1, castles, nil, `{"turn":30}`))
	if len(upgrades) != 1 || *upgrades[0].CastleID != 0 || *upgrades[0].UpgradeType != models.SpeedUpgrade {
		t.Fatalf("got upgrades %+v and sends %+v", upgrades, sends)
	}
}

func TestDefaultBotsPassWithoutCastles(t *testing.T) {
	castles := []models.CastleView{castle(0, 2, 30, 0, 0)}
	for _, bot := range bots.Defaults() {
		_, sends, upgrades := play(t, bot.Level, turn(1, castles, nil, ""))
		if len(sends)+len(upgrades) != 0 {
			t.Fatalf("level %d sent commands without castles", bot.Level)
		}
	}
}

func TestLevelThreeForgetsMemoryItCannotRead(t *testing.T) {
	castles := []models.CastleView{castle(0, 1, 30, 0, 0), castle(1, 2, 30, 4, 0)}
	result, _, _ := play(t, 3, turn(1, castles, nil, `{"turn":"soon"}`))
	if string(result.Memory) != `{"turn":1}` {
		t.Fatalf("got memory %s", result.Memory)
	}
}
