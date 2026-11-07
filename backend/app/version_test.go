package app

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHealthzReportsTheVersionAndKeeps204(t *testing.T) {
	recorder := httptest.NewRecorder()
	healthz("v1.2.3")(recorder, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("status=%d, want 204", recorder.Code)
	}
	if got := recorder.Header().Get(VersionHeader); got != "v1.2.3" {
		t.Fatalf("%s=%q, want v1.2.3", VersionHeader, got)
	}
}
