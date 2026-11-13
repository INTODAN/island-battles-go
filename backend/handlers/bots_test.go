package handlers_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"testing"
	"time"

	"islandBattles/backend/handlers"
	"islandBattles/backend/models"
	"islandBattles/backend/routes"
	"islandBattles/backend/services"

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

// GET /game/bot-compute needs no authentication, and reports the guard's
// month: on a server that doesn't measure the runners, available with 0
// used; past the budget, unavailable; and 503 when the read fails.
func TestGetBotCompute(t *testing.T) {
	now := func() time.Time { return time.Date(2026, time.November, 13, 10, 0, 0, 0, time.UTC) }
	for _, check := range []struct {
		guard *services.BotComputeGuard
		want  string
	}{
		{&services.BotComputeGuard{BudgetGBSeconds: 300000, Now: now}, `{"month":"2026-11","used_gb_seconds":0,"budget_gb_seconds":300000,"available":true}`},
		{&services.BotComputeGuard{Meter: services.FixedBotCompute(1200.5), BudgetGBSeconds: 300000, Now: now}, `{"month":"2026-11","used_gb_seconds":1200.5,"budget_gb_seconds":300000,"available":true}`},
		{&services.BotComputeGuard{Meter: services.FixedBotCompute(300000), BudgetGBSeconds: 300000, Now: now}, `{"month":"2026-11","used_gb_seconds":300000,"budget_gb_seconds":300000,"available":false}`},
	} {
		_, api := humatest.New(t)
		routes.SetupRoutes(api, &handlers.Env{BotCompute: check.guard}, "")
		response := api.Get("/game/bot-compute")
		var body, want map[string]any
		if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil || response.Code != http.StatusOK {
			t.Fatalf("status %d: %s", response.Code, response.Body.String())
		}
		delete(body, "$schema")
		if err := json.Unmarshal([]byte(check.want), &want); err != nil || !reflect.DeepEqual(body, want) {
			t.Errorf("got %s, want %s", response.Body.String(), check.want)
		}
	}
	_, api := humatest.New(t)
	routes.SetupRoutes(api, &handlers.Env{BotCompute: &services.BotComputeGuard{Meter: failingMeter{}, BudgetGBSeconds: 300000}}, "")
	if response := api.Get("/game/bot-compute"); response.Code != http.StatusServiceUnavailable {
		t.Fatalf("a failed read: status %d: %s", response.Code, response.Body.String())
	}
}

type failingMeter struct{}

func (failingMeter) MonthToDate(context.Context, time.Time, time.Time) (float64, error) {
	return 0, errors.New("throttled")
}
