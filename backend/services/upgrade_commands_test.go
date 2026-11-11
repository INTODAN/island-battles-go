package services_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"islandBattles/backend/models"
	"islandBattles/backend/services"
	"islandBattles/lobby/domain"
)

func upgrade(castleID uint, upgradeType models.UpgradeType) models.PublicGenerationCommand {
	raw, _ := json.Marshal(map[string]any{"castle_id": castleID, "upgrade_type": upgradeType})
	return models.PublicGenerationCommand{CommandType: models.UpgradeCastle, Parameters: raw}
}

// assertProblem checks a rejection's status, problem type and, when given, a
// part of its detail.
func assertProblem(t *testing.T, err error, status int, problemType, detail string) {
	t.Helper()
	var rejection *services.GenerationError
	if !errors.As(err, &rejection) || rejection.Status != status || rejection.Type != problemType || !strings.Contains(rejection.Detail, detail) {
		t.Fatalf("error=%#v, want status %d, type %q and detail containing %q", err, status, problemType, detail)
	}
}

// Castle 0 belongs to the first player's team and castle 1 to the other team;
// each starts with 30 soldiers.
func TestCommandRejectionsCarryProblemTypes(t *testing.T) {
	fixture := newTurnFixture(t)
	ctx := context.Background()
	raw := func(parameters string) models.PublicGenerationCommand {
		return models.PublicGenerationCommand{CommandType: models.UpgradeCastle, Parameters: json.RawMessage(parameters)}
	}
	cases := []struct {
		name        string
		commands    []models.PublicGenerationCommand
		status      int
		problemType string
		detail      string
	}{
		{"missing castle_id", []models.PublicGenerationCommand{raw(`{"upgrade_type":"speed"}`)}, 422, "", "upgrade_castle parameters"},
		{"missing upgrade_type", []models.PublicGenerationCommand{raw(`{"castle_id":0}`)}, 422, "", "upgrade_castle parameters"},
		{"string castle_id", []models.PublicGenerationCommand{raw(`{"castle_id":"0","upgrade_type":"speed"}`)}, 422, "", "upgrade_castle parameters"},
		{"numeric upgrade_type", []models.PublicGenerationCommand{raw(`{"castle_id":0,"upgrade_type":1}`)}, 422, "", "upgrade_castle parameters"},
		{"extra key", []models.PublicGenerationCommand{raw(`{"castle_id":0,"upgrade_type":"speed","level":2}`)}, 422, "", "upgrade_castle parameters"},
		{"null parameters", []models.PublicGenerationCommand{raw(`null`)}, 422, "", "upgrade_castle parameters"},
		{"unknown type", []models.PublicGenerationCommand{upgrade(0, "armor")}, 422, services.UnknownUpgradeTypeProblemType, `"armor"`},
		{"missing castle", []models.PublicGenerationCommand{upgrade(123, models.SpeedUpgrade)}, 422, services.CastleNotFoundProblemType, "castle 123"},
		{"enemy castle", []models.PublicGenerationCommand{upgrade(1, models.SpeedUpgrade)}, 403, services.CastleNotOwnedProblemType, "castle 1 belongs to team 2"},
		{"past the maximum in one batch", []models.PublicGenerationCommand{upgrade(0, models.ClashStopGenerationsUpgrade), upgrade(0, models.ClashStopGenerationsUpgrade), upgrade(0, models.ClashStopGenerationsUpgrade)}, 409, services.UpgradeMaxLevelProblemType, "clash_stop_generations level 3"},
		{"levels cost more than the castle has", []models.PublicGenerationCommand{upgrade(0, models.SpeedUpgrade), upgrade(0, models.SpeedUpgrade), upgrade(0, models.SpeedUpgrade)}, 409, services.InsufficientSoldiersProblemType, "castle 0 has 0 unreserved soldiers; speed level 4 costs 30"},
		{"a send reserves soldiers before an upgrade", []models.PublicGenerationCommand{send(0, 1, 25), upgrade(0, models.SpeedUpgrade)}, 409, services.InsufficientSoldiersProblemType, "castle 0 has 5 unreserved soldiers; speed level 2 costs 10"},
		{"an upgrade reserves soldiers before a send", []models.PublicGenerationCommand{upgrade(0, models.SpeedUpgrade), send(0, 1, 21)}, 409, services.InsufficientSoldiersProblemType, "castle 0 has 20 unreserved soldiers; sending 21"},
		{"send from an enemy castle", []models.PublicGenerationCommand{send(1, 0, 1)}, 403, services.CastleNotOwnedProblemType, "castle 1"},
		{"send to a missing castle", []models.PublicGenerationCommand{send(0, 123, 1)}, 422, services.CastleNotFoundProblemType, "castle 123"},
		{"send from a missing castle", []models.PublicGenerationCommand{send(123, 0, 1)}, 422, services.CastleNotFoundProblemType, "castle 123"},
		{"send more than the castle has", []models.PublicGenerationCommand{send(0, 1, 31)}, 409, services.InsufficientSoldiersProblemType, "castle 0 has 30 unreserved soldiers"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			_, err := fixture.service.Register(ctx, fixture.players[0], batch(fixture.state, false, test.commands...))
			assertProblem(t, err, test.status, test.problemType, test.detail)
		})
	}
	// Every rejected batch was dropped whole.
	assertTurnCounts(t, fixture, 1, 0, 0, 2)
}

// Every command that Register accepts, in any of a player's batches this turn,
// must still apply when the turn runs. A failing command there would roll back
// the whole turn.
func TestAcceptedSendsAndUpgradesInEarlierBatchesAlwaysApply(t *testing.T) {
	fixture := newTurnFixture(t)
	ctx := context.Background()
	player := fixture.players[0]
	register := func(player domain.Principal, complete bool, commands ...models.PublicGenerationCommand) (models.GenerationState, error) {
		return fixture.service.Register(ctx, player, batch(fixture.state, complete, commands...))
	}
	// Castle 0 has 30 soldiers: 10 + 5 in the first batch, 5 + 10 in the second.
	if _, err := register(player, false, upgrade(0, models.SpeedUpgrade), send(0, 1, 5)); err != nil {
		t.Fatal(err)
	}
	_, err := register(player, false, send(0, 1, 5), upgrade(0, models.SpeedUpgrade))
	assertProblem(t, err, 409, services.InsufficientSoldiersProblemType, "castle 0 has 10 unreserved soldiers; speed level 3 costs 20")
	if _, err := register(player, false, send(0, 1, 5), upgrade(0, models.ClashStopGenerationsUpgrade)); err != nil {
		t.Fatal(err)
	}
	// The castle's balance is spent exactly, so nothing more fits.
	_, err = register(player, false, upgrade(0, models.ClashStopGenerationsUpgrade))
	assertProblem(t, err, 409, services.InsufficientSoldiersProblemType, "castle 0 has 0 unreserved soldiers; clash_stop_generations level 3 costs 20")
	_, err = register(player, false, send(0, 1, 1))
	assertProblem(t, err, 409, services.InsufficientSoldiersProblemType, "castle 0 has 0 unreserved soldiers")
	if _, err := register(player, true); err != nil {
		t.Fatal(err)
	}
	next, err := register(fixture.players[1], true, upgrade(1, models.SpeedUpgrade), send(1, 0, 20))
	if err != nil {
		t.Fatalf("the turn failed after accepting every command: %v", err)
	}
	if next.GenerationID == fixture.state.GenerationID {
		t.Fatal("the turn did not advance")
	}
	// Every castle paid exactly its balance, then grew by one soldier.
	want := []models.Upgrades{{models.SpeedUpgrade: 2, models.ClashStopGenerationsUpgrade: 2}, {models.SpeedUpgrade: 2, models.ClashStopGenerationsUpgrade: 1}}
	for index, castle := range next.Generation.Castles {
		if castle.Soldiers != 1 || castle.Upgrades[models.SpeedUpgrade] != want[index][models.SpeedUpgrade] || castle.Upgrades[models.ClashStopGenerationsUpgrade] != want[index][models.ClashStopGenerationsUpgrade] {
			t.Fatalf("castle %d = %+v, want 1 soldier and %v", castle.ID, castle, want[index])
		}
	}
	sent := uint(0)
	for _, group := range next.Generation.SoldierGroups {
		sent += group.Soldiers
		if len(group.Upgrades) != 2 {
			t.Fatalf("group %d does not list every upgrade type: %v", group.ID, group.Upgrades)
		}
	}
	if len(next.Generation.SoldierGroups) != 3 || sent != 30 {
		t.Fatalf("groups = %+v", next.Generation.SoldierGroups)
	}
}

// Upgrades in a player's earlier batch count toward the level limit, and
// spending a castle's exact balance on them succeeds when the turn runs.
func TestUpgradesInEarlierBatchesCountTowardTheMaximumLevel(t *testing.T) {
	fixture := newTurnFixture(t)
	ctx := context.Background()
	if _, err := fixture.service.Register(ctx, fixture.players[0], batch(fixture.state, false, upgrade(0, models.ClashStopGenerationsUpgrade), upgrade(0, models.ClashStopGenerationsUpgrade))); err != nil {
		t.Fatal(err)
	}
	_, err := fixture.service.Register(ctx, fixture.players[0], batch(fixture.state, false, upgrade(0, models.ClashStopGenerationsUpgrade)))
	assertProblem(t, err, 409, services.UpgradeMaxLevelProblemType, "castle 0 already reaches clash_stop_generations level 3, the maximum")
	for _, player := range fixture.players {
		if _, err := fixture.service.Register(ctx, player, batch(fixture.state, true)); err != nil {
			t.Fatal(err)
		}
	}
	next, err := fixture.service.Current(ctx, fixture.players[0], fixture.state.LobbyID)
	if err != nil {
		t.Fatal(err)
	}
	castle := next.Generation.Castles[0]
	if next.GenerationID == fixture.state.GenerationID || castle.Soldiers != 1 || castle.Upgrades[models.ClashStopGenerationsUpgrade] != 3 {
		t.Fatalf("castle after spending its balance = %+v", castle)
	}
	_, err = fixture.service.Register(ctx, fixture.players[0], batch(next, true, upgrade(0, models.ClashStopGenerationsUpgrade)))
	assertProblem(t, err, 409, services.UpgradeMaxLevelProblemType, "clash_stop_generations level 3")
}
