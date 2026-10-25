package services_test

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"islandBattles/backend/dal"
	"islandBattles/backend/models"
	"islandBattles/backend/services"
)

// assertGameEnded checks that commands for a finished game are rejected with
// the game-ended problem type.
func assertGameEnded(t *testing.T, err error) {
	t.Helper()
	var rejection *services.GenerationError
	if !errors.As(err, &rejection) || rejection.Status != 409 || rejection.Type != services.GameEndedProblemType {
		t.Fatalf("error=%v, want a 409 of type %s", err, services.GameEndedProblemType)
	}
}

func TestGameEndsWithTheLastTeamStanding(t *testing.T) {
	fixture := newTurnFixture(t, false)
	ctx := context.Background()
	var attacker, defender models.CastleView
	for _, castle := range fixture.state.Generation.Castles {
		if castle.TeamColor == fixture.state.TeamColor {
			attacker = castle
		} else {
			defender = castle
		}
	}
	// Empty the defending castle, so the attackers capture it on arrival.
	initial, err := dal.GetGeneration(fixture.database, fixture.state.GenerationID)
	if err != nil {
		t.Fatal(err)
	}
	for _, castle := range initial.Map.Castles {
		if castle.PersistentCastleID == defender.ID {
			if err := fixture.database.Model(&castle).Select("SoldierAmount").Updates(&models.Castle{SoldierAmount: 0}).Error; err != nil {
				t.Fatal(err)
			}
		}
	}
	current := fixture.state
	var finalRequest models.RegisterGenerationCommands
	for turn := 0; current.Outcome == nil; turn++ {
		if turn == 20 {
			t.Fatal("the attack never ended the game")
		}
		var commands []models.PublicGenerationCommand
		if turn == 0 {
			commands = append(commands, send(attacker.ID, defender.ID, attacker.Soldiers))
		}
		if _, err := fixture.service.Register(ctx, fixture.players[0], batch(current, true, commands...)); err != nil {
			t.Fatal(err)
		}
		finalRequest = batch(current, true)
		if current, err = fixture.service.Register(ctx, fixture.players[1], finalRequest); err != nil {
			t.Fatal(err)
		}
	}
	game, err := dal.FindLobbyGame(fixture.database, fixture.state.LobbyID)
	if err != nil {
		t.Fatal(err)
	}
	var winner models.GameTeam
	for _, team := range game.Teams {
		if team.Color == fixture.state.TeamColor {
			winner = team
		}
	}
	if game.Outcome == nil || game.Outcome.Reason != models.GameOutcomeLastTeamStanding || game.Outcome.WinnerTeamColor == nil || *game.Outcome.WinnerTeamColor != fixture.state.TeamColor || winner.Color != fixture.state.TeamColor {
		t.Fatalf("saved outcome = %+v, teams = %+v", game.Outcome, game.Teams)
	}
	want := models.GameOutcomeView{Reason: models.GameOutcomeLastTeamStanding, WinnerTeamColor: game.Outcome.WinnerTeamColor, Summary: winner.Name + " wins as the last team standing"}
	if !reflect.DeepEqual(current.Outcome, &want) || current.Submitted {
		t.Fatalf("final state outcome = %+v submitted=%v, want %+v", current.Outcome, current.Submitted, want)
	}
	for _, castle := range current.Generation.Castles {
		if castle.TeamColor != fixture.state.TeamColor {
			t.Fatalf("castle %d was not captured: %+v", castle.ID, current.Generation)
		}
	}

	// The final generation is pushed once to every roster player as game.ended.
	ended := fixture.publisher.GameEndedEvents(t)
	if len(ended) != len(fixture.players) {
		t.Fatalf("pushed %d game.ended events, want %d", len(ended), len(fixture.players))
	}
	recipients := map[string]bool{}
	for _, event := range ended {
		if event.SchemaVersion != 1 || event.State.GenerationID != current.GenerationID || !reflect.DeepEqual(event.State.Outcome, &want) || event.State.Submitted {
			t.Fatalf("bad game.ended event: %+v", event)
		}
		recipients[event.State.PlayerID] = true
	}
	for _, player := range fixture.players {
		if !recipients[player.PlayerID] {
			t.Fatalf("game.ended was not pushed to %s", player.PlayerID)
		}
	}
	for _, event := range fixture.publisher.GenerationEvents(t) {
		if event.State.GenerationID == current.GenerationID || event.State.Outcome != nil {
			t.Fatalf("the final generation was also pushed as game.next_generation: %+v", event)
		}
	}

	generations, err := dal.CountGenerationsForGame(fixture.database, fixture.state.GameID)
	if err != nil {
		t.Fatal(err)
	}
	pushed := len(fixture.publisher.Publications())
	// Every new command is rejected, whatever generation it names.
	for _, player := range fixture.players {
		_, err = fixture.service.Register(ctx, player, batch(current, true))
		assertGameEnded(t, err)
		_, err = fixture.service.Register(ctx, player, batch(fixture.state, false, send(attacker.ID, defender.ID, 1)))
		assertGameEnded(t, err)
	}
	// The request that ended the game can still be retried.
	if retried, err := fixture.service.Register(ctx, fixture.players[1], finalRequest); err != nil || retried.GenerationID != current.GenerationID || retried.Outcome == nil {
		t.Fatalf("retry of the final request = %+v, %v", retried, err)
	}
	if after, err := dal.CountGenerationsForGame(fixture.database, fixture.state.GameID); err != nil || after != generations {
		t.Fatalf("a finished game has %d generations, had %d: %v", after, generations, err)
	}
	if len(fixture.publisher.Publications()) != pushed {
		t.Fatal("a finished game pushed more events")
	}

	// Reads keep working and show the outcome.
	for _, player := range fixture.players {
		state, err := fixture.service.Current(ctx, player, fixture.state.LobbyID)
		if err != nil || state.GenerationID != current.GenerationID || !reflect.DeepEqual(state.Outcome, &want) {
			t.Fatalf("finished game read = %+v, %v", state, err)
		}
	}
	replay, err := fixture.service.Replay(ctx, fixture.players[1], fixture.state.LobbyID)
	if err != nil {
		t.Fatal(err)
	}
	if !replay.Ended || replay.Outcome != want.Summary || replay.Frames[len(replay.Frames)-1].ID != current.GenerationID {
		t.Fatalf("replay ended=%v outcome=%q", replay.Ended, replay.Outcome)
	}
}
