package app

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"

	"github.com/go-chi/chi/v5"
)

// LoopbackOrigin is the URL that the game's game API client calls while the
// lobby runs in this process. Its requests never leave the process, so its
// host is never resolved.
const LoopbackOrigin = "http://localhost"

// Loopback carries HTTP requests in memory: a request to a URL under one of
// its prefixes is served by that prefix's handler, in this process, as if
// it had come over the network, and any other request goes to Fallback,
// http.DefaultTransport when nil. While the lobby and the game share a
// process, the lobby calls the game's hooks and the game calls the lobby's
// game API through it, so the two talk only HTTP without a network or a
// second Lambda invocation.
//
// A handler gets a copy of the request with the request's context, so it ends
// with the caller's timeout, and with RequestURI set, as a server sets it.
// Its answer is buffered.
type Loopback struct {
	routes   []loopbackRoute
	Fallback http.RoundTripper
}

type loopbackRoute struct {
	prefix  string
	handler func() http.Handler
}

// Handle serves requests whose URL starts with prefix, such as
// http://localhost/game/lobby-hooks, with the handler that handler returns
// at the time of each request, so the two sides can be built in either
// order.
func (loopback *Loopback) Handle(prefix string, handler func() http.Handler) {
	loopback.routes = append(loopback.routes, loopbackRoute{prefix: strings.TrimSuffix(prefix, "/"), handler: handler})
}

// Client is an HTTP client that sends through the loopback.
func (loopback *Loopback) Client() *http.Client {
	return &http.Client{Transport: loopback}
}

func (loopback *Loopback) RoundTrip(request *http.Request) (*http.Response, error) {
	target := request.URL.String()
	for _, route := range loopback.routes {
		if target != route.prefix && !strings.HasPrefix(target, route.prefix+"/") && !strings.HasPrefix(target, route.prefix+"?") {
			continue
		}
		handler := route.handler()
		if handler == nil {
			break
		}
		return serve(handler, request)
	}
	fallback := loopback.Fallback
	if fallback == nil {
		fallback = http.DefaultTransport
	}
	return fallback.RoundTrip(request)
}

func serve(handler http.Handler, request *http.Request) (*http.Response, error) {
	body := []byte{}
	if request.Body != nil {
		read, err := io.ReadAll(request.Body)
		_ = request.Body.Close()
		if err != nil {
			return nil, err
		}
		body = read
	}
	// The caller's context may carry the routing state of the request that
	// made this call, which a chi router would take for its own and route by.
	served := request.Clone(context.WithValue(request.Context(), chi.RouteCtxKey, nil))
	served.Body = io.NopCloser(bytes.NewReader(body))
	served.ContentLength = int64(len(body))
	served.RequestURI = request.URL.RequestURI()
	served.RemoteAddr = "127.0.0.1:0"
	served.Host = request.URL.Host
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, served)
	if err := request.Context().Err(); err != nil {
		return nil, err
	}
	response := recorder.Result()
	response.Request = request
	return response, nil
}
