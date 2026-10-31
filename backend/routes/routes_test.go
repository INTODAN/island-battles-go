package routes_test

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/danielgtaylor/huma/v2/humatest"
	"islandBattles/backend/handlers"
	"islandBattles/backend/models"
	"islandBattles/backend/routes"
	"islandBattles/lobby/domain"
	"net/http"
	"reflect"
	"strings"
	"testing"
)

func TestUpgradesAreListedWithoutAuthentication(t *testing.T) {
	_, api := humatest.New(t, routes.NewAPIConfig("test", "1.0.0"))
	routes.SetupRoutes(api, &handlers.Env{}, "")
	if security := api.OpenAPI().Paths["/game/upgrades"].Get.Security; len(security) != 0 {
		t.Fatalf("GET /game/upgrades requires %v", security)
	}
	response := api.Get("/game/upgrades")
	var body struct{ Upgrades []models.UpgradeView }
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil || response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s err=%v", response.Code, response.Body.String(), err)
	}
	if !reflect.DeepEqual(body.Upgrades, models.UpgradeTable()) {
		t.Fatalf("upgrades=%+v, want %+v", body.Upgrades, models.UpgradeTable())
	}
	// The map file docs take each type's levels from the same table.
	description := api.OpenAPI().Paths["/map/create"].Post.Description
	for _, upgrade := range models.UpgradeTable() {
		if want := fmt.Sprintf("%s supports levels 1 through %d", upgrade.Type, upgrade.MaxLevel); !strings.Contains(description, want) {
			t.Fatalf("/map/create description lacks %q: %s", want, description)
		}
	}
}

func TestMapCreateAcceptsTheMapFileFormat(t *testing.T) {
	_, api := humatest.New(t, routes.NewAPIConfig("test", "1.0.0"))
	routes.SetupRoutes(api, &handlers.Env{}, "admin-key")
	body := api.OpenAPI().Paths["/map/create"].Post.RequestBody.Content["application/json"].Schema
	if body.Ref != "#/components/schemas/MapTemplate" {
		t.Fatalf("/map/create body is %q, not the MapTemplate schema", body.Ref)
	}
	if _, linked := api.OpenAPI().Components.Schemas.SchemaFromRef(body.Ref).Properties["$schema"]; linked {
		t.Fatal("the map file format publishes Huma's $schema link property")
	}
	// Validation rejects the body before the handler would reach the database.
	linkedFile := `{"$schema":"https://example.com/MapTemplate.json","map_name":"linked","player_amount":1,"plane_size_x":2,"plane_size_y":2,"neutral_starting_soldier_amount":1,"castles":[{"team_color":1,"x":0,"y":0}]}`
	if response := api.Post("/map/create", routes.AdminAPIKeyHeader+": admin-key", strings.NewReader(linkedFile)); response.Code != http.StatusUnprocessableEntity || !strings.Contains(response.Body.String(), "body.$schema") {
		t.Fatalf("a map file with $schema got status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestMapValidateDocumentsTheMapFileFormat(t *testing.T) {
	_, api := humatest.New(t, routes.NewAPIConfig("test", "1.0.0"))
	routes.SetupRoutes(api, &handlers.Env{}, "")
	content := api.OpenAPI().Paths["/map/validate"].Post.RequestBody.Content
	if len(content) != 1 || content["application/json"].Schema.Ref != "#/components/schemas/MapTemplate" {
		t.Fatalf("/map/validate body is not documented as the MapTemplate schema: %+v", content)
	}
}

func TestMapCreateRequiresTheAdminAPIKey(t *testing.T) {
	// Every body here is invalid, so a request that got past the key check
	// would answer 422 before reaching the missing database.
	for name, check := range map[string]struct {
		configured string
		headers    []any
		status     int
	}{
		"no key sent":                {"admin-key", nil, http.StatusUnauthorized},
		"wrong key":                  {"admin-key", []any{routes.AdminAPIKeyHeader + ": admin-kez"}, http.StatusUnauthorized},
		"prefix of the key":          {"admin-key", []any{routes.AdminAPIKeyHeader + ": admin-ke"}, http.StatusUnauthorized},
		"key as a bearer token":      {"admin-key", []any{"Authorization: Bearer admin-key"}, http.StatusUnauthorized},
		"no key configured":          {"", []any{routes.AdminAPIKeyHeader + ": "}, http.StatusForbidden},
		"no key configured, any key": {"", []any{routes.AdminAPIKeyHeader + ": admin-key"}, http.StatusForbidden},
		"right key":                  {"admin-key", []any{routes.AdminAPIKeyHeader + ": admin-key"}, http.StatusUnprocessableEntity},
	} {
		t.Run(name, func(t *testing.T) {
			_, api := humatest.New(t, routes.NewAPIConfig("test", "1.0.0"))
			routes.SetupRoutes(api, &handlers.Env{}, check.configured)
			if response := api.Post("/map/create", append(check.headers, strings.NewReader(`{}`))...); response.Code != check.status {
				t.Fatalf("status=%d, want %d: %s", response.Code, check.status, response.Body.String())
			}
		})
	}
	_, api := humatest.New(t, routes.NewAPIConfig("test", "1.0.0"))
	routes.SetupRoutes(api, &handlers.Env{}, "admin-key")
	operation := api.OpenAPI().Paths["/map/create"].Post
	if len(operation.Security) != 1 || operation.Security[0]["AdminAPIKey"] == nil {
		t.Fatalf("/map/create security=%v", operation.Security)
	}
	if scheme := api.OpenAPI().Components.SecuritySchemes["AdminAPIKey"]; scheme == nil || scheme.Type != "apiKey" || scheme.In != "header" || scheme.Name != routes.AdminAPIKeyHeader {
		t.Fatalf("AdminAPIKey scheme=%+v", scheme)
	}
}

func TestTheGameGraphIsRegisteredSeparately(t *testing.T) {
	_, api := humatest.New(t)
	routes.SetupRoutes(api, &handlers.Env{}, "")
	if _, exists := api.OpenAPI().Paths["/game/{id}/graph"]; exists {
		t.Fatal("SetupRoutes registers the graph page, which every host would then serve")
	}
	routes.SetupGameGraphRoute(api, &handlers.Env{})
	if _, exists := api.OpenAPI().Paths["/game/{id}/graph"]; !exists {
		t.Fatal("SetupGameGraphRoute doesn't register the graph page")
	}
}

func TestRawGenerationMutationRoutesAreRemoved(t *testing.T) {
	_, api := humatest.New(t)
	routes.SetupRoutes(api, &handlers.Env{}, "")
	for _, path := range []string{"/generation/create", "/generation/migrate"} {
		if _, exists := api.OpenAPI().Paths[path]; exists {
			t.Fatalf("public bypass route remains: %s", path)
		}
		if response := api.Post(path, `{}`); response.Code != 404 {
			t.Fatalf("%s status=%d", path, response.Code)
		}
	}
}

// rejectEveryone authenticates nobody.
type rejectEveryone struct{}

func (rejectEveryone) CreateGuest(context.Context, domain.CreateGuestRequest) (domain.GuestSession, error) {
	return domain.GuestSession{}, domain.ErrUnauthenticated
}
func (rejectEveryone) Authenticate(context.Context, string) (domain.SessionVerification, error) {
	return domain.SessionVerification{}, domain.ErrUnauthenticated
}

// The logs read documents generation_id as required and positive, but checks
// it only after authenticating and authorizing the caller, so a caller off
// the roster learns nothing from a 422. From the review of PR #86.
func TestTheLogsReadAuthenticatesBeforeCheckingTheGeneration(t *testing.T) {
	_, api := humatest.New(t, routes.NewAPIConfig("test", "1.0.0"))
	routes.SetupLobbyRoutes(api, nil, nil, nil, nil, &handlers.LobbyGame{Authenticator: rejectEveryone{}})
	path := "/game/from-lobby/4a0f1a52-7f4e-4d5c-9a8e-2f3b6c1d0e9a/logs"
	for _, query := range []string{"?generation_id=0", ""} {
		if response := api.Get(path+query, "Authorization: Bearer stranger"); response.Code != http.StatusUnauthorized {
			t.Fatalf("%s: status=%d %s", query, response.Code, response.Body.String())
		}
	}
	for _, parameter := range api.OpenAPI().Paths["/game/from-lobby/{lobby_id}/logs"].Get.Parameters {
		if parameter.Name == "generation_id" && (!parameter.Required || parameter.Schema.Minimum == nil || *parameter.Schema.Minimum != 1) {
			t.Fatalf("generation_id is documented as %+v", parameter)
		}
	}
}
