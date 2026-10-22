package services_test

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"islandBattles/backend/dal"
	"islandBattles/backend/models"
	"islandBattles/backend/services"

	"gorm.io/gorm"
)

func TestClashEventsPersistPerContactAndDoNotCarryForward(t *testing.T) {
	database, generation := captureFixture(t)
	captureGroup(t, database, generation, 1, 10, 1)
	captureGroup(t, database, generation, 2, 1, 1.25)
	captureGroup(t, database, generation, 2, 20, 1.75)
	generationID, err := services.MigrateGeneration(database, generation.GameID, nil)
	if err != nil {
		t.Fatal(err)
	}
	result, err := dal.GetGeneration(database, generationID)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Clashes) != 2 {
		t.Fatalf("expected two clashes: %+v", result.Clashes)
	}
	first, second := result.Clashes[0], result.Clashes[1]
	if first.ID != 1 || second.ID != 2 || first.Time != 0 || second.Time <= first.Time {
		t.Fatalf("invalid ordered event IDs/times: %+v", result.Clashes)
	}
	if first.Participants[0].GroupID != second.Participants[0].GroupID ||
		first.Participants[0].SoldiersBefore != 10 || first.Participants[0].SoldiersAfter != 9 ||
		second.Participants[0].SoldiersBefore != 9 || second.Participants[0].SoldiersAfter != 0 {
		t.Fatalf("repeated participant lost per-contact counts: %+v", result.Clashes)
	}
	if first.Participants[1].SoldiersBefore != 1 || first.Participants[1].SoldiersAfter != 0 ||
		second.Participants[1].SoldiersBefore != 20 || second.Participants[1].SoldiersAfter != 11 {
		t.Fatalf("incorrect enemy casualties: %+v", result.Clashes)
	}
	if first.Participants[0].X != 1 || first.Participants[1].X != 1.25 || first.Participants[0].Y != 1 || first.Participants[0].ClashRadius != .25 {
		t.Fatalf("lost contact geometry: %+v", first)
	}
	if len(result.SoldierGroups) != 1 {
		t.Fatal("defeated groups remain active")
	}
	nextID, err := services.MigrateGeneration(database, generation.GameID, nil)
	if err != nil {
		t.Fatal(err)
	}
	next, err := dal.GetGeneration(database, nextID)
	if err != nil {
		t.Fatal(err)
	}
	if len(next.Clashes) != 0 {
		t.Fatal("old clash effects carried into next generation")
	}
	previous, err := dal.GetGeneration(database, generationID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(previous.Clashes, result.Clashes) {
		t.Fatal("clash history was mutated")
	}
}

func TestClashEventsReachSnapshotsReplayAndNotifications(t *testing.T) {
	for _, enemyAmount := range []uint{10, 20} {
		t.Run(map[uint]string{10: "survivor", 20: "mutual defeat"}[enemyAmount], func(t *testing.T) {
			fixture := newTurnFixture(t, false)
			ctx := context.Background()
			state := fixture.state
			var finalRequest models.RegisterGenerationCommands
			for turn := 0; turn < 4; turn++ {
				first, second := batch(state, true), batch(state, true)
				if turn == 0 {
					first.Commands = []models.PublicGenerationCommand{send(0, 1, 20)}
					second.Commands = []models.PublicGenerationCommand{send(1, 0, enemyAmount)}
				}
				if _, err := fixture.service.Register(ctx, fixture.players[0], first); err != nil {
					t.Fatal(err)
				}
				var err error
				state, err = fixture.service.Register(ctx, fixture.players[1], second)
				if err != nil {
					t.Fatal(err)
				}
				finalRequest = second
			}
			if len(state.Generation.Clashes) != 1 {
				t.Fatalf("missing clash: %+v", state.Generation)
			}
			clash := state.Generation.Clashes[0]
			if len(clash.Participants) != 2 {
				t.Fatalf("bad casualties: %+v", clash)
			}
			for _, participant := range clash.Participants {
				before, after := enemyAmount, uint(0)
				if participant.TeamColor == fixture.state.TeamColor {
					before, after = 20, 20-enemyAmount
				}
				if participant.SoldiersBefore != before || participant.SoldiersAfter != after {
					t.Fatalf("bad casualties: %+v", clash)
				}
			}
			current, err := fixture.service.Current(ctx, fixture.players[0], state.LobbyID)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(current.Generation.Clashes, state.Generation.Clashes) {
				t.Fatal("reloaded snapshot lost clash events")
			}
			retry, err := fixture.service.Register(ctx, fixture.players[1], finalRequest)
			if err != nil {
				t.Fatal(err)
			}
			if retry.GenerationID != state.GenerationID || !reflect.DeepEqual(retry.Generation.Clashes, state.Generation.Clashes) {
				t.Fatal("retry changed clash events")
			}
			game, err := dal.GetGameByID(fixture.database, state.GameID)
			if err != nil {
				t.Fatal(err)
			}
			var replay struct {
				Frames []struct {
					Clashes []models.Clash `json:"clashes"`
				} `json:"frames"`
			}
			if err := json.Unmarshal(game.Replay, &replay); err != nil {
				t.Fatal(err)
			}
			if len(replay.Frames) != 5 || !reflect.DeepEqual(replay.Frames[4].Clashes, state.Generation.Clashes) {
				t.Fatal("saved replay lost or duplicated events")
			}
			var notifications []models.GenerationNotification
			if err := fixture.database.Where(&models.GenerationNotification{GenerationID: state.GenerationID}).Find(&notifications).Error; err != nil {
				t.Fatal(err)
			}
			if len(notifications) != 2 {
				t.Fatal("expected a notification for each player")
			}
			for _, notification := range notifications {
				var event struct {
					State models.GenerationState `json:"state"`
				}
				if err := json.Unmarshal(notification.Payload, &event); err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(event.State.Generation.Clashes, state.Generation.Clashes) {
					t.Fatal("live notification lost events")
				}
			}
		})
	}
}

func TestClashEventsRollBackWithFailedTurn(t *testing.T) {
	database, generation := captureFixture(t)
	captureGroup(t, database, generation, 1, 10, 1)
	captureGroup(t, database, generation, 2, 5, 1.25)
	failure := errors.New("cannot delete defeated troop")
	callback := "test:fail-after-clash-events"
	if err := database.Callback().Delete().Before("gorm:delete").Register(callback, func(transaction *gorm.DB) {
		if transaction.Statement.Table == "soldier_groups" {
			transaction.AddError(failure)
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Callback().Delete().Remove(callback) })
	if _, err := services.MigrateGeneration(database, generation.GameID, nil); !errors.Is(err, failure) {
		t.Fatalf("expected injected failure, got %v", err)
	}
	var generations []models.Generation
	if err := database.Find(&generations).Error; err != nil {
		t.Fatal(err)
	}
	if len(generations) != 1 || len(generations[0].Clashes) != 0 {
		t.Fatalf("failed turn left clash history: %+v", generations)
	}
}
