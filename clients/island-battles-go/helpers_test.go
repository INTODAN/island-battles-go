package islandbattles

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestCommandsHaveTheContractsJSON(t *testing.T) {
	for _, test := range []struct {
		command Command
		want    string
	}{
		{SendSoldierGroup(0, 3, 12), `{"command_type":"send_soldier_group","parameters":{"origin_castle_id":0,"destination_castle_id":3,"soldier_amount":12}}`},
		{UpgradeCastle(2, SpeedUpgrade), `{"command_type":"upgrade_castle","parameters":{"castle_id":2,"upgrade_type":"speed"}}`},
	} {
		if encoded, err := json.Marshal(test.command); err != nil || string(encoded) != test.want {
			t.Errorf("got %s (%v), want %s", encoded, err, test.want)
		}
	}
}

func TestNextUpgradeCostReadsTheTurnsTable(t *testing.T) {
	turn := FixtureTurn()
	castle := turn.Generation.Castles[0]
	if cost, found := NextUpgradeCost(turn, castle, SpeedUpgrade); !found || cost != 10 {
		t.Fatalf("level 2 of speed costs %d (%v), want 10", cost, found)
	}
	castle.Upgrades = Upgrades{SpeedUpgrade: 3}
	if cost, found := NextUpgradeCost(turn, castle, SpeedUpgrade); !found || cost != 30 {
		t.Fatalf("level 4 of speed costs %d (%v), want 30", cost, found)
	}
	// A level the map doesn't list is level 1.
	if cost, found := NextUpgradeCost(turn, castle, ClashStopGenerationsUpgrade); !found || cost != 10 {
		t.Fatalf("level 2 of clash_stop_generations costs %d (%v), want 10", cost, found)
	}
	castle.Upgrades = Upgrades{ClashStopGenerationsUpgrade: 3}
	if _, found := NextUpgradeCost(turn, castle, ClashStopGenerationsUpgrade); found {
		t.Fatal("a castle at the maximum level has a next level")
	}
	if level, found := turn.UpgradeLevel(SpeedUpgrade, 4); !found || level.Value != 2 {
		t.Fatalf("speed level 4 is %+v (%v)", level, found)
	}
}

func TestLogLinesAreCutAndCleaned(t *testing.T) {
	line := Log("attacking")
	if line != (LogLine{Level: LogInfo, Logger: PlayerLogger, Message: "attacking"}) {
		t.Fatalf("got %+v", line)
	}
	if line := Logf("castle %d", 3); line.Message != "castle 3" || line.Level != LogInfo {
		t.Fatalf("got %+v", line)
	}
	if line := LogAt(LogWarning, "a\x00b\xffc"); line.Message != "a�b�c" || line.Level != LogWarning {
		t.Fatalf("got %+v", line)
	}
	long := LogAt(LogError, strings.Repeat("é", 600)).Message
	if utf8.RuneCountInString(long) != MaxLogMessageLength || !strings.HasSuffix(long, "… (cut at 500 characters)") {
		t.Fatalf("got a message of %d characters ending %q", utf8.RuneCountInString(long), long[len(long)-40:])
	}
	if exact := strings.Repeat("x", MaxLogMessageLength); Log(exact).Message != exact {
		t.Fatal("a message of exactly the limit was cut")
	}
}

func TestEntitiesCompareTeamsAndDistances(t *testing.T) {
	turn := FixtureTurn()
	generation := turn.Generation
	own, enemy, neutral := generation.Castles[0], generation.Castles[1], generation.Castles[2]
	group := generation.SoldierGroups[0]
	for _, test := range []struct {
		got, want TeamType
	}{
		{own.CompareTeam(own), Friendly},
		{own.CompareTeam(enemy), Enemy},
		{own.CompareTeam(neutral), Neutral},
		{neutral.CompareTeam(neutral), Neutral},
		{neutral.CompareTeam(own), Enemy},
		{group.CompareTeam(enemy), Friendly},
		{enemy.CompareTeam(group), Friendly},
		{group.CompareTeam(own), Enemy},
	} {
		if test.got != test.want {
			t.Errorf("got %s, want %s", test.got, test.want)
		}
	}
	if distance := own.Distance(enemy); distance != 6 {
		t.Fatalf("castles 0 and 1 are %v apart", distance)
	}
	if distance := group.Distance(own); distance != 4 || own.Distance(group) != 4 {
		t.Fatalf("the group is %v from castle 0", distance)
	}
	if destination, found := group.DestinationCastle(generation); !found || destination.ID != 0 {
		t.Fatalf("the destination is %+v (%v)", destination, found)
	}
	if left, found := group.DistanceLeft(generation); !found || left != 4 {
		t.Fatalf("the distance left is %v (%v)", left, found)
	}
	group.DestinationCastleID = 99
	if _, found := group.DistanceLeft(generation); found {
		t.Fatal("a missing destination has a distance")
	}
	if _, found := generation.Castle(99); found {
		t.Fatal("found castle 99")
	}
}

func TestMemoryDecodesAndEncodes(t *testing.T) {
	type state struct {
		Target int `json:"target"`
	}
	remembered := state{Target: 5}
	if err := Memory(nil).Decode(&remembered); err != nil || remembered.Target != 5 {
		t.Fatalf("no memory changed the state to %+v (%v)", remembered, err)
	}
	if err := Memory(`{"target": 3}`).Decode(&remembered); err != nil || remembered.Target != 3 {
		t.Fatalf("got %+v (%v)", remembered, err)
	}
	memory, err := NewMemory(state{Target: 4})
	if err != nil || string(memory) != `{"target":4}` {
		t.Fatalf("got %s (%v)", memory, err)
	}
	if memory, err := NewMemory(nil); err != nil || memory != nil {
		t.Fatalf("nil is %s (%v), want no memory", memory, err)
	}
	if _, err := NewMemory([]int{1}); !errors.Is(err, ErrInvalidResult) {
		t.Fatalf("an array gave %v", err)
	}
	if _, err := NewMemory(map[string]string{"big": strings.Repeat("x", MemoryLimitBytes)}); !errors.Is(err, ErrMemoryTooLarge) {
		t.Fatalf("too much memory gave %v", err)
	}
	if size, err := Memory(nil).Size(); err != nil || size != 4 {
		t.Fatalf("no memory is %d bytes (%v)", size, err)
	}
	if size, err := Memory(`{ "a" : 1 }`).Size(); err != nil || size != 7 {
		t.Fatalf("the memory is %d bytes (%v)", size, err)
	}
	var turn Turn
	if err := json.Unmarshal([]byte(`{"memory": null}`), &turn); err != nil || turn.Memory != nil {
		t.Fatalf("null memory is %s (%v)", turn.Memory, err)
	}
}

func TestResultsWriteEmptyListsAsArrays(t *testing.T) {
	if encoded, err := json.Marshal(Result{}); err != nil || string(encoded) != `{"commands":[],"memory":null,"logs":[]}` {
		t.Fatalf("got %s (%v)", encoded, err)
	}
}

func TestValidateResult(t *testing.T) {
	valid := Result{Commands: []Command{SendSoldierGroup(0, 1, 1), UpgradeCastle(0, ClashStopGenerationsUpgrade)}, Logs: []LogLine{Log("ok")}, Memory: Memory(`{"a":1}`)}
	if err := ValidateResult(valid); err != nil {
		t.Fatal(err)
	}
	if err := ValidateResult(Result{}); err != nil {
		t.Fatal(err)
	}
	tooMany := func(count int, line LogLine) []LogLine {
		lines := make([]LogLine, count)
		for index := range lines {
			lines[index] = line
		}
		return lines
	}
	for name, result := range map[string]Result{
		"zero soldiers":     {Commands: []Command{SendSoldierGroup(0, 1, 0)}},
		"unknown upgrade":   {Commands: []Command{UpgradeCastle(0, "armour")}},
		"unknown command":   {Commands: []Command{{CommandType: "build", Parameters: json.RawMessage(`{}`)}}},
		"missing parameter": {Commands: []Command{{CommandType: SendSoldierGroupCommand, Parameters: json.RawMessage(`{"origin_castle_id":0,"destination_castle_id":1}`)}}},
		"null parameter":    {Commands: []Command{{CommandType: UpgradeCastleCommand, Parameters: json.RawMessage(`{"castle_id":null,"upgrade_type":"speed"}`)}}},
		"unknown parameter": {Commands: []Command{{CommandType: UpgradeCastleCommand, Parameters: json.RawMessage(`{"castle_id":0,"upgrade_type":"speed","level":2}`)}}},
		"no parameters":     {Commands: []Command{{CommandType: UpgradeCastleCommand}}},
		"too many commands": {Commands: make([]Command, MaxCommands+1)},
		"too many lines":    {Logs: tooMany(MaxLogLines+1, Log("x"))},
		"empty message":     {Logs: []LogLine{Log("")}},
		"long message":      {Logs: []LogLine{{Level: LogInfo, Logger: PlayerLogger, Message: strings.Repeat("x", 501)}}},
		"long logger":       {Logs: []LogLine{{Level: LogInfo, Logger: strings.Repeat("x", 101), Message: "x"}}},
		"unknown level":     {Logs: []LogLine{{Level: "fatal", Logger: PlayerLogger, Message: "x"}}},
		"invalid UTF-8":     {Logs: []LogLine{{Level: LogInfo, Logger: PlayerLogger, Message: "\xff"}}},
		"memory not object": {Memory: Memory(`[1]`)},
		"memory not JSON":   {Memory: Memory(`{`)},
	} {
		if err := ValidateResult(result); !errors.Is(err, ErrInvalidResult) {
			t.Errorf("%s: got %v", name, err)
		}
	}
	big := Result{Memory: Memory(`{"big":"` + strings.Repeat("x", MemoryLimitBytes) + `"}`)}
	if err := ValidateResult(big); !errors.Is(err, ErrMemoryTooLarge) {
		t.Fatalf("too much memory gave %v", err)
	}
	if err := ValidateResult(Result{Logs: tooMany(MaxLogLines, Log("x"))}); err != nil {
		t.Fatalf("100 lines gave %v", err)
	}
}

func TestDecodeTurn(t *testing.T) {
	turn, err := DecodeTurn(fixtureTurnJSON)
	if err != nil || turn.Generation.ID != 12 || len(turn.Generation.Castles) != 4 || turn.Memory != nil || turn.TurnDeadlineAt != nil {
		t.Fatalf("got %+v (%v)", turn, err)
	}
	fixture := func(change func(map[string]any)) []byte {
		var data map[string]any
		_ = json.Unmarshal(fixtureTurnJSON, &data)
		change(data)
		encoded, _ := json.Marshal(data)
		return encoded
	}
	generation := func(data map[string]any) map[string]any { return data["generation"].(map[string]any) }
	if _, err := DecodeTurn(fixture(func(data map[string]any) { data["extra"] = true; data["memory"] = map[string]any{"a": 1} })); err != nil {
		t.Fatalf("an unknown field and memory gave %v", err)
	}
	for name, body := range map[string][]byte{
		"not JSON":          []byte("{"),
		"an array":          []byte("[]"),
		"no game":           fixture(func(data map[string]any) { delete(data, "game_id") }),
		"game 0":            fixture(func(data map[string]any) { data["game_id"] = 0 }),
		"team 0":            fixture(func(data map[string]any) { data["team_color"] = 0 }),
		"no memory field":   fixture(func(data map[string]any) { delete(data, "memory") }),
		"no deadline field": fixture(func(data map[string]any) { delete(data, "turn_deadline_at") }),
		"null upgrades":     fixture(func(data map[string]any) { data["upgrades"] = nil }),
		"memory an array":   fixture(func(data map[string]any) { data["memory"] = []int{1} }),
		"no castles":        fixture(func(data map[string]any) { delete(generation(data), "castles") }),
		"castle without x":  fixture(func(data map[string]any) { delete(generation(data)["castles"].([]any)[0].(map[string]any), "x") }),
		"a string soldiers": fixture(func(data map[string]any) {
			generation(data)["castles"].([]any)[0].(map[string]any)["soldiers"] = "many"
		}),
		"missing destination": fixture(func(data map[string]any) {
			generation(data)["soldier_groups"].([]any)[0].(map[string]any)["destination_castle_id"] = 99
		}),
		"generation 0": fixture(func(data map[string]any) { generation(data)["id"] = 0 }),
		"group without radius": fixture(func(data map[string]any) {
			delete(generation(data)["soldier_groups"].([]any)[0].(map[string]any), "clash_radius")
		}),
	} {
		if _, err := DecodeTurn(body); err == nil {
			t.Errorf("%s: decoded", name)
		}
	}
}
