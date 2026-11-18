package models

import "testing"

func TestAManualPlayerIsNeitherAServerBotNorALocalhostBot(t *testing.T) {
	if ManualBot.PlayedByServer() {
		t.Fatal("the server plays a manual player")
	}
	port := uint(41415)
	view := FrozenBot(FrozenGamePlayer{BotKind: ManualBot, BotPort: &port})
	if view.Kind != ManualBot || view.Port != nil || view.Level != nil || view.Host != "" {
		t.Fatalf("FrozenBot = %+v", view)
	}
}
