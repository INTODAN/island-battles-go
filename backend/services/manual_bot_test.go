package services

import (
	"testing"

	"islandBattles/backend/models"
)

// Nothing plays a manual player: not the Go bots loop, not a runner, and no
// client relays them. Their own client sends their commands.
func TestNothingPlaysOrSendsToAManualPlayer(t *testing.T) {
	player := models.FrozenGamePlayer{PlayerID: "manual", BotKind: models.ManualBot}
	if isGoBot(player) || passesEveryTurn(player) || sentByServer(player) {
		t.Fatal("the server plays a manual player")
	}
	if relayed := models.RelayedBots([]models.FrozenGamePlayer{player}, "manual"); len(relayed) != 0 {
		t.Fatalf("a client relays a manual player: %+v", relayed)
	}
}

func TestTheFreezeCopiesAManualKindAndNothingElse(t *testing.T) {
	var player models.FrozenGamePlayer
	if err := (frozenBot{Kind: models.ManualBot}).freeze(nil, "lobby", &player); err != nil {
		t.Fatal(err)
	}
	if player.BotKind != models.ManualBot || player.BotPort != nil || player.BotLevel != nil || player.BotURL != nil || player.BotPackageID != nil || player.BotRelayPlayerID != nil {
		t.Fatalf("frozen player = %+v", player)
	}
}
