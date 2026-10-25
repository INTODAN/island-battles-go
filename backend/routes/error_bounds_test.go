package routes_test

import (
	"encoding/json"
	"fmt"
	"islandBattles/backend/handlers"
	"islandBattles/backend/routes"
	"islandBattles/lobby/httpapi"
	"net/http"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2/humatest"
)

func TestMapCreateBoundsAProblemForUnknownKeys(t *testing.T) {
	_, api := humatest.New(t, routes.NewAPIConfig("test", "1.0.0"))
	routes.SetupRoutes(api, &handlers.Env{})
	var body strings.Builder
	body.WriteString(`{"map_name":"unknown keys"`)
	for index := range 2000 {
		fmt.Fprintf(&body, `,"unknown%05d":0`, index)
	}
	body.WriteString("}")
	// Validation rejects the body before the handler would reach the database.
	response := api.Post("/map/create", strings.NewReader(body.String()))
	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status %d", response.Code)
	}
	// Unbounded, each of the 2000 errors echoes the whole 30 KB body.
	if size := response.Body.Len(); size > 16<<10 {
		t.Fatalf("problem is %d bytes for a %d byte body", size, body.Len())
	}
	var problem struct {
		Status int              `json:"status"`
		Errors []map[string]any `json:"errors"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &problem); err != nil {
		t.Fatal(err)
	}
	if problem.Status != http.StatusUnprocessableEntity || len(problem.Errors) != httpapi.MaxErrorDetails {
		t.Fatalf("status=%d errors=%d", problem.Status, len(problem.Errors))
	}
	for _, detail := range problem.Errors {
		if _, echoed := detail["value"]; echoed {
			t.Fatalf("detail echoes its object: %v", detail)
		}
	}
	if last := fmt.Sprint(problem.Errors[len(problem.Errors)-1]["message"]); !strings.HasSuffix(last, " more errors") {
		t.Fatalf("last detail = %q", last)
	}
}
