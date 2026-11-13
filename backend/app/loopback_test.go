package app

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (send roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return send(request)
}

// A request through the loopback reaches the handler of its prefix as a
// server request, also when the call is made while a chi router serves
// another request, whose routing state its context carries.
func TestLoopbackServesEachPrefixInMemory(t *testing.T) {
	inner := chi.NewMux()
	inner.Post("/lobby/v1/game-api/games/{key}/echo", func(response http.ResponseWriter, request *http.Request) {
		body, _ := io.ReadAll(request.Body)
		_, _ = io.WriteString(response, request.RequestURI+" "+chi.URLParam(request, "key")+" "+string(body))
	})
	loopback := &Loopback{Fallback: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		return nil, errors.New("sent to the network: " + request.URL.String())
	})}
	loopback.Handle(LoopbackOrigin+"/lobby/v1/game-api", func() http.Handler { return inner })
	client := loopback.Client()

	outer := chi.NewMux()
	var answer string
	outer.Get("/game/from-lobby/{lobby_id}/watch", func(response http.ResponseWriter, request *http.Request) {
		call, _ := http.NewRequestWithContext(request.Context(), http.MethodPost, LoopbackOrigin+"/lobby/v1/game-api/games/mock/echo?x=1", strings.NewReader("hello"))
		served, err := client.Do(call)
		if err != nil {
			t.Error(err)
			return
		}
		defer func() { _ = served.Body.Close() }()
		body, _ := io.ReadAll(served.Body)
		answer = served.Status + " " + string(body)
	})
	outer.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/game/from-lobby/abc/watch", nil))
	if answer != "200 OK /lobby/v1/game-api/games/mock/echo?x=1 mock hello" {
		t.Fatalf("answer = %q", answer)
	}

	for _, target := range []string{"http://localhost/lobby/v1/game-apix", "https://lobby.example.test/lobby/v1/game-api/games/mock/echo"} {
		if _, err := client.Post(target, "text/plain", strings.NewReader("")); err == nil || !strings.Contains(err.Error(), "sent to the network") {
			t.Errorf("%s: err = %v", target, err)
		}
	}
}
