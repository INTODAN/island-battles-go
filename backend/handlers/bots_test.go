package handlers_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"islandBattles/backend/handlers"
	"islandBattles/backend/models"
	"islandBattles/backend/routes"

	"github.com/danielgtaylor/huma/v2/humatest"
)

func TestListDefaultBotsHTTP(t *testing.T) {
	_, api := humatest.New(t)
	routes.SetupRoutes(api, &handlers.Env{}, "")
	response := api.Get("/bots/defaults")
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var body struct {
		Bots []models.DefaultBotView `json:"bots"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Bots) != 3 {
		t.Fatalf("got %d bots, want 3: %s", len(body.Bots), response.Body.String())
	}
	for index, bot := range body.Bots {
		if bot.Level != uint(index+1) || bot.Name == "" || bot.Description == "" {
			t.Fatalf("unexpected bot %+v", bot)
		}
	}
}

// server_bots reports ISLAND_BATTLES_SERVER_BOTS, which is off by default.
func TestListDefaultBotsSaysWhetherServerBotsAreOffered(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		_, api := humatest.New(t)
		routes.SetupRoutes(api, &handlers.Env{ServerBots: enabled}, "")
		var body struct {
			ServerBots *bool `json:"server_bots"`
		}
		response := api.Get("/bots/defaults")
		if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil || body.ServerBots == nil || *body.ServerBots != enabled {
			t.Fatalf("enabled %v: %s, %v", enabled, response.Body.String(), err)
		}
	}
}
