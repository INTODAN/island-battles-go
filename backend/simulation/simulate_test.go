package simulation_test

import (
	"errors"
	"reflect"
	"testing"

	"islandBattles/backend/models"
	"islandBattles/backend/simulation"

	"github.com/jackc/pgx/v5/pgtype"
)

func point(x, y float64) pgtype.Point {
	return pgtype.Point{P: pgtype.Vec2{X: x, Y: y}, Valid: true}
}

// newState builds a board like a fresh map: every castle starts with soldiers
// and default upgrades. Castle IDs follow the order of the teams.
func newState(soldiers uint, castles ...simulation.Castle) simulation.GameState {
	state := simulation.GameState{Castles: map[uint]*simulation.Castle{}, SoldierGroups: map[uint]*simulation.SoldierGroup{}}
	for id, castle := range castles {
		castle.ID, castle.Soldiers, castle.Upgrades = uint(id), soldiers, castle.Upgrades.Clone()
		state.Castles[castle.ID] = &castle
	}
	return state
}

// captureState has team 1's castle 0 at (0,1) and team 2's castle 1 at (3,1).
func captureState() simulation.GameState {
	return newState(100, simulation.Castle{TeamColor: 1, Position: point(0, 1)}, simulation.Castle{TeamColor: 2, Position: point(3, 1)})
}

// placeGroup puts a group of team on the road between the two castles of
// captureState, heading for the other team's castle.
func placeGroup(state *simulation.GameState, team, soldiers uint, x float64) {
	origin, destination := uint(0), uint(1)
	if team == 2 {
		origin, destination = 1, 0
	}
	id := state.NextSoldierGroupID
	state.NextSoldierGroupID++
	state.SoldierGroups[id] = &simulation.SoldierGroup{
		ID: id, TeamColor: team, Soldiers: soldiers, Position: point(x, 1), OriginCastleID: origin, DestinationCastleID: destination,
		ClashRadius: models.DefaultClashRadius, Upgrades: models.Upgrades{}.Clone(),
	}
}

func simulate(t *testing.T, state simulation.GameState, commands ...simulation.Command) (simulation.GameState, []simulation.Event) {
	t.Helper()
	next, events, err := simulation.Simulate(state, commands)
	if err != nil {
		t.Fatal(err)
	}
	return next, events
}

// playTurn runs the commands followed by the steps every turn takes.
func playTurn(t *testing.T, state simulation.GameState, commands ...simulation.Command) simulation.GameState {
	t.Helper()
	endOfTurn, err := simulation.DecodeCommands(simulation.EndOfTurnCommands())
	if err != nil {
		t.Fatal(err)
	}
	next, _ := simulate(t, state, append(commands, endOfTurn...)...)
	return next
}

func TestArrivedArmyDoesNotClashWithNewSpawn(t *testing.T) {
	state := captureState()
	placeGroup(&state, 1, 120, 3)
	state, events := simulate(t, state,
		simulation.SendSoldierGroupParams{OriginPersistentCastleID: 1, DestinationPersistentCastleID: 0, SoldierAmount: 1},
		simulation.MoveSoldierGroupsParams{}, simulation.RemoveArrivedSoldierGroupsParams{})
	defender := state.Castles[1]
	if defender.TeamColor != 1 || defender.Soldiers != 21 {
		t.Fatalf("arrival incorrectly clashed with spawn: %+v", defender)
	}
	groups := state.SortedSoldierGroups()
	if len(groups) != 1 || groups[0].TeamColor != 2 || groups[0].Soldiers != 1 {
		t.Fatal("new spawn should remain a separate traveling group")
	}
	if len(events) != 0 {
		t.Fatal("arrival incorrectly emitted a clash event")
	}
}

func TestMultipleArrivalsUseUpdatedCastle(t *testing.T) {
	state := captureState()
	placeGroup(&state, 1, 60, 3)
	placeGroup(&state, 1, 60, 3)
	placeGroup(&state, 1, 10, 3)
	state, _ = simulate(t, state, simulation.RemoveArrivedSoldierGroupsParams{})
	defender := state.Castles[1]
	if defender.TeamColor != 1 || defender.Soldiers != 30 || len(state.SoldierGroups) != 0 {
		t.Fatalf("arrival damage/capture was overwritten: %+v", defender)
	}
}

func TestSurvivorCannotBypassSecondDefender(t *testing.T) {
	state := captureState()
	placeGroup(&state, 1, 10, 1)
	placeGroup(&state, 2, 1, 1.25)
	placeGroup(&state, 2, 20, 1.75)
	state, _ = simulate(t, state, simulation.MoveSoldierGroupsParams{})
	groups := state.SortedSoldierGroups()
	if len(groups) != 1 || groups[0].TeamColor != 2 || groups[0].Soldiers != 11 || groups[0].Position.P.X != 1.25 {
		t.Fatalf("later combat was bypassed or survivor stopped: %+v", groups)
	}
}

func TestEmptyAndSelfSendOrdersAreRejected(t *testing.T) {
	state := captureState()
	for _, parameters := range []simulation.SendSoldierGroupParams{{OriginPersistentCastleID: 0, DestinationPersistentCastleID: 1, SoldierAmount: 0}, {OriginPersistentCastleID: 0, DestinationPersistentCastleID: 0, SoldierAmount: 1}} {
		if _, _, err := simulation.Simulate(state, []simulation.Command{parameters}); err == nil {
			t.Fatalf("invalid send accepted: %+v", parameters)
		}
	}
	if state.Castles[0].Soldiers != 100 || len(state.SoldierGroups) != 0 {
		t.Fatal("rejected send changed game state")
	}
}

func TestSimulateLeavesItsInputUnchanged(t *testing.T) {
	state := captureState()
	placeGroup(&state, 1, 10, 1)
	placeGroup(&state, 2, 5, 1.25)
	before := state.Clone()
	next := playTurn(t, state,
		simulation.UpgradeCastleParams{PersistentCastleID: 0, UpgradeType: models.SpeedUpgrade},
		simulation.SendSoldierGroupParams{OriginPersistentCastleID: 0, DestinationPersistentCastleID: 1, SoldierAmount: 10})
	if !reflect.DeepEqual(state, before) {
		t.Fatal("Simulate changed the state it was given")
	}
	if reflect.DeepEqual(next, before) {
		t.Fatal("the turn changed nothing")
	}
}

// A captured castle's in-flight groups keep fighting for the team that sent them.
func TestSoldierGroupColor(t *testing.T) {
	state := newState(15,
		simulation.Castle{TeamColor: 1, Position: point(0, 1)},
		simulation.Castle{TeamColor: 2, Position: point(4, 1)},
		simulation.Castle{TeamColor: 2, Position: point(2, 2)})
	// Castle 2 needs enough soldiers to attack and capture castle 0 later.
	state.Castles[2].Soldiers = 40
	initialColor := state.Castles[0].TeamColor

	// Turn 1: castle 0 (team 1) sends group X (5 soldiers) to castle 1 (team 2).
	// The distance is 4, so it arrives on turn 8.
	state = playTurn(t, state, simulation.SendSoldierGroupParams{OriginPersistentCastleID: 0, DestinationPersistentCastleID: 1, SoldierAmount: 5})
	// Turn 2: castle 2 (team 2) sends group Y (30 soldiers) to castle 0, arriving on turn 6.
	state = playTurn(t, state, simulation.SendSoldierGroupParams{OriginPersistentCastleID: 2, DestinationPersistentCastleID: 0, SoldierAmount: 30})
	for range 4 {
		state = playTurn(t, state)
	}
	if state.Castles[0].TeamColor != 2 {
		t.Fatalf("Expected Castle 0 to be captured by Team 2, but got Team %d", state.Castles[0].TeamColor)
	}
	var groupX *simulation.SoldierGroup
	for _, group := range state.SoldierGroups {
		if group.OriginCastleID == 0 && group.DestinationCastleID == 1 {
			groupX = group
		}
	}
	if groupX == nil {
		t.Fatal("Expected Group X to be in flight, but it was not found")
	}
	if groupX.TeamColor != initialColor {
		t.Fatalf("Allegiance mismatch: Group X team color was changed to %d; expected original color %d", groupX.TeamColor, initialColor)
	}

	// Turn 7 is the last one before group X arrives at castle 1.
	state = playTurn(t, state)
	soldiersBeforeArrival := state.Castles[1].Soldiers
	state = playTurn(t, state)
	soldiersAfterArrival := state.Castles[1].Soldiers
	if soldiersAfterArrival >= soldiersBeforeArrival {
		t.Fatalf("Expected destination castle soldier amount to decrease, but before was %d and after was %d", soldiersBeforeArrival, soldiersAfterArrival)
	}
	// Castle 1 had 22 soldiers on turn 7. On turn 8 it grows to 23 and group X's
	// 5 soldiers attack it, leaving 18.
	if soldiersAfterArrival != 18 {
		t.Fatalf("Expected destination castle soldier amount after attack to be 18, got %d", soldiersAfterArrival)
	}
}

func TestSentGroupsTakeNewIDsAndSnapshotTheirCastle(t *testing.T) {
	state := captureState()
	state.NextSoldierGroupID = 7
	state.Castles[0].Upgrades[models.SpeedUpgrade] = 3
	send := simulation.SendSoldierGroupParams{OriginPersistentCastleID: 0, DestinationPersistentCastleID: 1, SoldierAmount: 4}
	state, _ = simulate(t, state, send, send)
	if state.NextSoldierGroupID != 9 || state.Castles[0].Soldiers != 92 {
		t.Fatalf("next ID=%d castle soldiers=%d", state.NextSoldierGroupID, state.Castles[0].Soldiers)
	}
	for _, id := range []uint{7, 8} {
		group := state.SoldierGroups[id]
		if group == nil || group.ID != id || group.TeamColor != 1 || group.Soldiers != 4 || group.Position != point(0, 1) ||
			group.ClashRadius != models.DefaultClashRadius || group.Upgrades[models.SpeedUpgrade] != 3 || group.MovementPauseGenerations != 0 {
			t.Fatalf("group %d: %+v", id, group)
		}
	}
	// A departed group's ID is never handed out again.
	delete(state.SoldierGroups, 8)
	state, _ = simulate(t, state, send)
	if _, reused := state.SoldierGroups[8]; reused || state.SoldierGroups[9] == nil {
		t.Fatalf("departed group ID reused: %+v", state.SoldierGroups)
	}
	state.Castles[0].Soldiers = 3
	if _, _, err := simulation.Simulate(state, []simulation.Command{send}); err == nil {
		t.Fatal("sent more soldiers than the castle has")
	}
}

func TestUpgradeCostAndAffordability(t *testing.T) {
	for index, cost := range []uint{10, 20, 30} {
		level := index + 1
		state := captureState()
		castle := state.Castles[0]
		castle.Upgrades[models.SpeedUpgrade] = level
		castle.Soldiers = cost - 1
		upgrade := simulation.UpgradeCastleParams{PersistentCastleID: 0, UpgradeType: models.SpeedUpgrade}
		if _, _, err := simulation.Simulate(state, []simulation.Command{upgrade}); !errors.Is(err, models.ErrInsufficientSoldiers) {
			t.Fatalf("accepted unaffordable upgrade: %v", err)
		}
		if castle.Soldiers != cost-1 || castle.Upgrades[models.SpeedUpgrade] != level {
			t.Fatal("failed purchase mutated input")
		}
		castle.Soldiers = cost
		next, _ := simulate(t, state, upgrade)
		if purchased := next.Castles[0]; purchased.Soldiers != 0 || purchased.Upgrades[models.SpeedUpgrade] != level+1 {
			t.Fatalf("exact payment failed: %+v", purchased)
		}
	}
	state := captureState()
	state.Castles[0].Upgrades[models.SpeedUpgrade] = 4
	for _, rejected := range []struct {
		upgradeType models.UpgradeType
		expected    error
	}{{models.SpeedUpgrade, models.ErrUpgradeMaxLevel}, {"unknown", models.ErrInvalidUpgrade}} {
		upgrade := simulation.UpgradeCastleParams{PersistentCastleID: 0, UpgradeType: rejected.upgradeType}
		if _, _, err := simulation.Simulate(state, []simulation.Command{upgrade}); !errors.Is(err, rejected.expected) {
			t.Fatalf("%s: %v", rejected.upgradeType, err)
		}
	}
}

func TestEveryCastleGrowsEachTurn(t *testing.T) {
	state := newState(15, simulation.Castle{TeamColor: 1}, simulation.Castle{TeamColor: models.NeutralTeamColor, Position: point(1, 0)})
	state, _ = simulate(t, state, simulation.IncreaseSoldierAmountParams{})
	if state.Castles[0].Soldiers != 16 || state.Castles[1].Soldiers != 16 {
		t.Fatalf("castles did not grow: %+v %+v", state.Castles[0], state.Castles[1])
	}
}

func TestArrivalReinforcesDamagesOrCapturesTheCastle(t *testing.T) {
	for _, scenario := range []struct {
		name             string
		team, soldiers   uint
		expectedTeam     uint
		expectedSoldiers uint
	}{
		{"friendly reinforcement", 2, 30, 2, 130},
		{"attack short of capture", 1, 60, 2, 40},
		{"attack that exactly empties the castle", 1, 100, 2, 0},
		{"capture", 1, 130, 1, 30},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			state := captureState()
			placeGroup(&state, scenario.team, scenario.soldiers, 3)
			state.SoldierGroups[0].DestinationCastleID = 1
			placeGroup(&state, 1, 5, 2)
			state, _ = simulate(t, state, simulation.RemoveArrivedSoldierGroupsParams{})
			castle := state.Castles[1]
			if castle.TeamColor != scenario.expectedTeam || castle.Soldiers != scenario.expectedSoldiers {
				t.Fatalf("castle: %+v", castle)
			}
			if len(state.SoldierGroups) != 1 || state.SoldierGroups[1] == nil {
				t.Fatal("only the arrived group leaves the field")
			}
		})
	}
}

func TestClashEventsAreReturnedAndNumberedAcrossTheTurn(t *testing.T) {
	state := captureState()
	placeGroup(&state, 1, 10, 1)
	placeGroup(&state, 2, 1, 1.25)
	placeGroup(&state, 2, 20, 1.75)
	state, events := simulate(t, state, simulation.MoveSoldierGroupsParams{})
	clashes := simulation.Clashes(events)
	if len(events) != 2 || len(clashes) != 2 || clashes[0].ID != 1 || clashes[1].ID != 2 || clashes[0].Time != 0 || clashes[1].Time <= clashes[0].Time {
		t.Fatalf("clash events: %+v", events)
	}
	first, second := clashes[0].Participants, clashes[1].Participants
	if first[0].GroupID != 0 || first[0].SoldiersBefore != 10 || first[0].SoldiersAfter != 9 || first[1].SoldiersBefore != 1 || first[1].SoldiersAfter != 0 ||
		second[0].SoldiersBefore != 9 || second[0].SoldiersAfter != 0 || second[1].SoldiersBefore != 20 || second[1].SoldiersAfter != 11 {
		t.Fatalf("clash participants: %+v", clashes)
	}
	if len(state.SoldierGroups) != 1 {
		t.Fatal("defeated groups remain active")
	}

	// Group 0 meets group 1 during the first move and group 2 during the second,
	// so the second move's clash continues the turn's numbering.
	state = captureState()
	placeGroup(&state, 1, 10, 1)
	placeGroup(&state, 2, 1, 1.25)
	placeGroup(&state, 2, 20, 2.75)
	_, events = simulate(t, state, simulation.MoveSoldierGroupsParams{}, simulation.MoveSoldierGroupsParams{})
	clashes = simulation.Clashes(events)
	if len(clashes) != 2 || clashes[0].ID != 1 || clashes[1].ID != 2 || clashes[1].Participants[1].GroupID != 2 {
		t.Fatalf("clash IDs must be unique within the turn: %+v", clashes)
	}
	if len(simulation.Clashes(nil)) != 0 || simulation.Clashes(nil) == nil {
		t.Fatal("a turn without clashes publishes an empty list")
	}
}

func TestTeamCounts(t *testing.T) {
	state := newState(1,
		simulation.Castle{TeamColor: 1}, simulation.Castle{TeamColor: 2, Position: point(1, 0)},
		simulation.Castle{TeamColor: 2, Position: point(2, 0)}, simulation.Castle{TeamColor: models.NeutralTeamColor, Position: point(3, 0)})
	placeGroup(&state, 1, 5, 1)
	placeGroup(&state, 1, 5, 2)
	if counts := state.CastleCounts(); !reflect.DeepEqual(counts, map[uint]int{0: 1, 1: 1, 2: 2}) {
		t.Fatalf("castle counts: %v", counts)
	}
	if counts := state.SoldierGroupCounts(); !reflect.DeepEqual(counts, map[uint]int{1: 2}) {
		t.Fatalf("group counts: %v", counts)
	}
}

func TestDecodeCommands(t *testing.T) {
	commands, err := simulation.DecodeCommands(append([]models.CommandFields{
		{CommandType: models.SendSoldierGroup, Parameters: []byte(`{"origin_castle_id":1,"destination_castle_id":2,"soldier_amount":3}`)},
		{CommandType: models.UpgradeCastle, Parameters: []byte(`{"castle_id":4,"upgrade_type":"speed"}`)},
	}, simulation.EndOfTurnCommands()...))
	if err != nil {
		t.Fatal(err)
	}
	expected := []simulation.Command{
		simulation.SendSoldierGroupParams{OriginPersistentCastleID: 1, DestinationPersistentCastleID: 2, SoldierAmount: 3},
		simulation.UpgradeCastleParams{PersistentCastleID: 4, UpgradeType: models.SpeedUpgrade},
		simulation.MoveSoldierGroupsParams{}, simulation.IncreaseSoldierAmountParams{}, simulation.RemoveArrivedSoldierGroupsParams{},
	}
	if !reflect.DeepEqual(commands, expected) {
		t.Fatalf("decoded %+v", commands)
	}
	for _, invalid := range []models.CommandFields{{CommandType: "unknown", Parameters: []byte("{}")}, {CommandType: models.SendSoldierGroup, Parameters: []byte("[")}} {
		if _, err := simulation.DecodeCommand(invalid); err == nil {
			t.Fatalf("decoded %+v", invalid)
		}
	}
}
