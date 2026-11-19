package simulation_test

import (
	"encoding/json"
	"reflect"
	"testing"

	"islandBattles/backend/models"
	"islandBattles/backend/simulation"
)

// A team that leaves a real-time game is out at once: its castles turn
// neutral and keep their soldiers and upgrades but not their burst debt, and
// its soldier groups vanish without a clash. Other teams keep theirs.
func TestRemoveTeamTurnsItsCastlesNeutralAndRemovesItsGroups(t *testing.T) {
	upgrades := models.Upgrades{models.GenerationAmountUpgrade: 2}
	state := newState(20, simulation.Castle{TeamColor: 1, Upgrades: upgrades, BurstDebt: 4}, simulation.Castle{TeamColor: 2, Position: point(3, 1)}, simulation.Castle{TeamColor: 1, Position: point(6, 1)})
	placeGroup(&state, 1, 5, 1)
	placeGroup(&state, 2, 7, 2)

	next, events := simulate(t, state, simulation.RemoveTeamParams{TeamColor: 1})
	if len(events) != 0 {
		t.Fatalf("events = %+v", events)
	}
	for _, id := range []uint{0, 2} {
		castle := next.Castles[id]
		if castle.TeamColor != models.NeutralTeamColor || castle.Soldiers != 20 || castle.BurstDebt != 0 {
			t.Fatalf("castle %d = %+v, want neutral with its 20 soldiers and no debt", id, castle)
		}
	}
	if next.Castles[0].Upgrades[models.GenerationAmountUpgrade] != 2 {
		t.Fatalf("castle 0 lost its upgrades: %+v", next.Castles[0].Upgrades)
	}
	if next.Castles[1].TeamColor != 2 {
		t.Fatal("the other team's castle changed hands")
	}
	if len(next.SoldierGroups) != 1 || next.SoldierGroups[1] == nil || next.SoldierGroups[1].TeamColor != 2 {
		t.Fatalf("groups = %+v, want only team 2's", next.SoldierGroups)
	}
	if counts := next.CastleCounts(); counts[1] != 0 {
		t.Fatalf("team 1 still owns %d castles", counts[1])
	}

	// From then on the castles are neutral: they produce nothing.
	after := playTurn(t, next)
	if after.Castles[0].Soldiers != 20 || after.Castles[2].Soldiers != 20 {
		t.Fatalf("the neutral castles grew: %d and %d", after.Castles[0].Soldiers, after.Castles[2].Soldiers)
	}

	if _, _, err := simulation.Simulate(state, []simulation.Command{simulation.RemoveTeamParams{TeamColor: models.NeutralTeamColor}}); err == nil {
		t.Fatal("removing the neutral colour succeeded")
	}
}

// The removal decodes from its stored form, so a stored turn reads back.
func TestRemoveTeamDecodes(t *testing.T) {
	command, err := simulation.DecodeCommand(models.CommandFields{CommandType: models.RemoveTeam, Parameters: []byte(`{"team_color":3}`)})
	if err != nil {
		t.Fatal(err)
	}
	if command != (simulation.RemoveTeamParams{TeamColor: 3}) {
		t.Fatalf("decoded %+v", command)
	}
}

// The leaving teams leave first, in colour order, their own commands of the
// turn are dropped, and the other teams' commands keep their order.
func TestTurnCommandsPutTheLeavingTeamsFirstAndDropTheirCommands(t *testing.T) {
	command := func(name string) models.CommandFields {
		return models.CommandFields{CommandType: models.SendSoldierGroup, Parameters: []byte(`"` + name + `"`)}
	}
	batches := []simulation.TeamBatch{
		{Color: 2, Commands: []models.CommandFields{command("b1"), command("b2")}},
		{Color: 3, Commands: []models.CommandFields{command("c1")}},
		{Color: 1, Commands: []models.CommandFields{command("a1")}},
	}
	got, err := simulation.TurnCommands([]uint{3, 1}, batches)
	if err != nil {
		t.Fatal(err)
	}
	var removed []uint
	for _, command := range got[:2] {
		if command.CommandType != models.RemoveTeam {
			t.Fatalf("the turn starts with %+v", got)
		}
		var parameters simulation.RemoveTeamParams
		if err := json.Unmarshal(command.Parameters, &parameters); err != nil {
			t.Fatal(err)
		}
		removed = append(removed, parameters.TeamColor)
	}
	if !reflect.DeepEqual(removed, []uint{1, 3}) {
		t.Fatalf("removed %v, want 1 then 3", removed)
	}
	if !reflect.DeepEqual(got[2:], []models.CommandFields{command("b1"), command("b2")}) {
		t.Fatalf("the rest = %+v, want team 2's commands in order", got[2:])
	}

	// Without a leaving team, the turn is the batches in order.
	unchanged, err := simulation.TurnCommands(nil, batches)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(unchanged, []models.CommandFields{command("b1"), command("b2"), command("c1"), command("a1")}) {
		t.Fatalf("without a leaving team = %+v", unchanged)
	}
}

// After the removal the end rule runs as after any turn: with two teams the
// other wins, and with three the game goes on.
func TestARemovedTeamIsOutForTheEndRule(t *testing.T) {
	two := board([]uint{1, 2, models.NeutralTeamColor}, 1)
	next, _ := simulate(t, two, simulation.RemoveTeamParams{TeamColor: 1})
	if got := simulation.Outcome(next, teams(1, 2), false); !reflect.DeepEqual(got, winner(models.GameOutcomeLastTeamStanding, 2)) {
		t.Fatalf("two teams: %s", describe(got))
	}
	three := board([]uint{1, 2, 3})
	next, _ = simulate(t, three, simulation.RemoveTeamParams{TeamColor: 1})
	if got := simulation.Outcome(next, teams(1, 2, 3), false); got != nil {
		t.Fatalf("three teams: %s", describe(got))
	}
	both, _ := simulate(t, board([]uint{1, 2}), simulation.RemoveTeamParams{TeamColor: 1}, simulation.RemoveTeamParams{TeamColor: 2})
	if got := simulation.Outcome(both, teams(1, 2), false); !reflect.DeepEqual(got, &models.GameOutcome{Reason: models.GameOutcomeAllEliminated}) {
		t.Fatalf("both teams: %s", describe(got))
	}
}

func TestAbandonedOutcome(t *testing.T) {
	neutral := models.NeutralTeamColor
	draw := &models.GameOutcome{Reason: models.GameOutcomeAbandoned}
	for _, test := range []struct {
		name     string
		state    simulation.GameState
		teams    []models.GameTeam
		resigned []uint
		want     *models.GameOutcome
	}{
		{"the most castles win", board([]uint{1, 1, 2}), teams(1, 2), nil, winner(models.GameOutcomeAbandoned, 1)},
		{"equal castle counts draw", board([]uint{1, 2, neutral}), teams(1, 2), nil, draw},
		{"a team whose resignation waits can't win", board([]uint{1, 1, 1, 2}), teams(1, 2), []uint{1}, winner(models.GameOutcomeAbandoned, 2)},
		{"no team left is a draw", board([]uint{1, 2}), teams(1, 2), []uint{1, 2}, draw},
		{"an eliminated team never wins", board([]uint{2, neutral}), teams(1, 2, 3), nil, winner(models.GameOutcomeAbandoned, 2)},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := simulation.AbandonedOutcome(test.state, test.teams, test.resigned); !reflect.DeepEqual(got, test.want) {
				t.Fatalf("outcome = %s, want %s", describe(got), describe(test.want))
			}
		})
	}
}
