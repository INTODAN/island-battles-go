package routes_test

import (
	"encoding/json"
	"fmt"
	"islandBattles/backend/handlers"
	"islandBattles/backend/models"
	"islandBattles/backend/routes"
	"islandBattles/lobby/httpapi"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/humatest"
)

// unknownKeys is how many unknown keys the bounded-problem tests send. Before
// the bound, 400 keys answered 1.8 MB and allocated about 64 MB, growing with
// the square of the count, so a regression stays far from the memory cap.
const unknownKeys = 400

// testAdminAPIKey lets /map/create requests through the admin check, so they
// reach validation.
const testAdminAPIKey = "admin-key"

type problem struct {
	Code   string             `json:"code"`
	Status int                `json:"status"`
	Errors []huma.ErrorDetail `json:"errors"`
}

// newBackendAPI serves the backend routes and the lobby API through the
// backend's API config, so both its transformer and the lobby's run.
func newBackendAPI(t *testing.T) http.Handler {
	handler, api := humatest.New(t, routes.NewAPIConfig("test", "1.0.0"))
	routes.SetupRoutes(api, &handlers.Env{}, testAdminAPIKey)
	// Validation rejects these requests before any service is called.
	httpapi.Register(api, nil, nil, nil, nil, nil)
	return handler
}

// serveProblem sends one request straight to handler, without humatest's
// logging, and fails unless the answer stays within 16 KiB and the request
// allocates at most 4 MiB.
func serveProblem(t *testing.T, handler http.Handler, path, body string) (int, problem) {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set(routes.AdminAPIKeyHeader, testAdminAPIKey)
	recorder := httptest.NewRecorder()
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	handler.ServeHTTP(recorder, request)
	runtime.ReadMemStats(&after)
	size, allocated := recorder.Body.Len(), after.TotalAlloc-before.TotalAlloc
	if size > 16<<10 || allocated > 4<<20 {
		t.Fatalf("%s answered %d bytes and allocated %d bytes for a %d byte body", path, size, allocated, len(body))
	}
	var answer problem
	if err := json.Unmarshal(recorder.Body.Bytes(), &answer); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	return recorder.Code, answer
}

func unknownKeysBody(fields string) string {
	var body strings.Builder
	body.WriteString("{" + fields)
	for index := range unknownKeys {
		fmt.Fprintf(&body, `,"unknown%05d":0`, index)
	}
	body.WriteString("}")
	return body.String()
}

// checkCount checks that a capped problem lists its other problems first and
// counts every problem it leaves out.
func checkCount(t *testing.T, answer problem, total int) {
	t.Helper()
	if len(answer.Errors) != models.MaxErrorDetails {
		t.Fatalf("%d errors, want %d", len(answer.Errors), models.MaxErrorDetails)
	}
	listed := answer.Errors[:models.MaxErrorDetails-1]
	for index, detail := range listed {
		if detail.Value != nil {
			t.Fatalf("detail echoes a value: %+v", detail)
		}
		if index > 0 && detail.Message != "unexpected property" && listed[index-1].Message == "unexpected property" {
			t.Fatalf("problem %+v is listed after an unknown key", detail)
		}
	}
	if last := answer.Errors[models.MaxErrorDetails-1]; last.Location != "" || last.Message != fmt.Sprintf("and %d more problems", total-len(listed)) {
		t.Fatalf("last detail = %+v, want and %d more problems", last, total-len(listed))
	}
}

func TestMapCreateBoundsAProblemForUnknownKeys(t *testing.T) {
	status, answer := serveProblem(t, newBackendAPI(t), "/map/create", unknownKeysBody(`"map_name":"unknown keys"`))
	if status != http.StatusUnprocessableEntity {
		t.Fatalf("status %d", status)
	}
	required := 0
	for _, detail := range answer.Errors {
		if strings.HasPrefix(detail.Message, "expected required property") {
			required++
		}
	}
	if required == 0 {
		t.Fatal("the missing required fields are not listed before the unknown keys")
	}
	checkCount(t, answer, required+unknownKeys)
}

func TestMapCreateCountsHandlerProblemsPastTheCap(t *testing.T) {
	const farCastles = 150
	castles := make([]string, farCastles)
	for index := range castles {
		castles[index] = fmt.Sprintf(`{"team_color":%d,"x":5,"y":0}`, index%2+1)
	}
	body := `{"map_name":"far","player_amount":2,"plane_size_x":2,"plane_size_y":2,"neutral_starting_soldier_amount":1,"castles":[` + strings.Join(castles, ",") + `]}`
	status, answer := serveProblem(t, newBackendAPI(t), "/map/create", body)
	if status != http.StatusUnprocessableEntity || answer.Errors[0].Location != "body.castles[0].x" {
		t.Fatalf("status=%d first=%+v", status, answer.Errors[0])
	}
	if len(answer.Errors) != models.MaxErrorDetails {
		t.Fatalf("%d errors", len(answer.Errors))
	}
	if last := answer.Errors[models.MaxErrorDetails-1]; last.Message != fmt.Sprintf("and %d more problems", farCastles-(models.MaxErrorDetails-1)) {
		t.Fatalf("last detail = %+v", last)
	}
}

func TestLobbyRoutesInTheBackendGiveABoundedProblem(t *testing.T) {
	status, answer := serveProblem(t, newBackendAPI(t), "/lobby/v1/sessions", unknownKeysBody(`"display_name":"Guest"`))
	if status != http.StatusUnprocessableEntity || answer.Code != "invalid_input" {
		t.Fatalf("status=%d code=%q", status, answer.Code)
	}
	checkCount(t, answer, unknownKeys)
}
