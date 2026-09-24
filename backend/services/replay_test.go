package services_test

import (
	"encoding/json"
	"strings"
	"testing"

	"islandBattles/backend/services"
)

func TestReplayEscapesPlayerNames(t *testing.T) {
	// JSONB can return literal angle brackets even when the encoder escaped them.
	replay := []byte(`{"game_id":1,"teams":[{"name":"</script><script>alert(1)</script>"}],"frames":[],"outcome":""}`)
	if !json.Valid(replay) {
		t.Fatal("invalid fixture")
	}
	html, err := services.RenderGameReplay(replay)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(html, "<script>alert(1)</script>") {
		t.Fatal("unescaped user text in replay HTML")
	}
	if !strings.Contains(html, `\u003c/script\u003e`) {
		t.Fatal("expected escaped script terminator")
	}
}
