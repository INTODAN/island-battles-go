package islandbattles

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"testing"
	"time"
)

// clockAt is a clock a test moves by hand.
type clockAt struct {
	mutex sync.Mutex
	now   time.Time
}

func (clock *clockAt) read() time.Time {
	clock.mutex.Lock()
	defer clock.mutex.Unlock()
	return clock.now
}

func (clock *clockAt) set(now time.Time) {
	clock.mutex.Lock()
	defer clock.mutex.Unlock()
	clock.now = now
}

func passTurn(context.Context, Turn) (Result, error) { return Result{}, nil }

func (fake *fakeServer) tokensIssued() int {
	fake.mutex.Lock()
	defer fake.mutex.Unlock()
	return fake.issued
}

func TestGameRequestsCarryAPlayerTokenThatIsRenewedAtEightyPercentOfItsLife(t *testing.T) {
	server := newFakeServer(t)
	start := time.Now()
	clock := &clockAt{now: start}
	client := testClient(t, server, &fakeRealtime{}, passTurn, func(client *Client) { client.timing.now = clock.read })
	ctx := context.Background()
	read := func() {
		t.Helper()
		if _, err := client.request(ctx, http.MethodGet, client.generationPath(), nil, false); err != nil {
			t.Fatal(err)
		}
	}
	read()
	clock.set(start.Add(11*time.Minute + 59*time.Second))
	read()
	if issued := server.tokensIssued(); issued != 1 {
		t.Fatalf("tokens issued within 80 %% of the first one's life = %d, want 1", issued)
	}
	clock.set(start.Add(12*time.Minute + time.Second))
	read()
	read()
	if issued := server.tokensIssued(); issued != 2 {
		t.Fatalf("tokens issued = %d, want 2", issued)
	}
	// The lobby's own routes still take the guest credential.
	if _, err := client.request(ctx, http.MethodPost, "/lobby/v1/connection-token", struct{}{}, false); err != nil {
		t.Fatal(err)
	}
}

func TestARefusedPlayerTokenIsRenewedOnce(t *testing.T) {
	server := newFakeServer(t)
	client := testClient(t, server, &fakeRealtime{}, passTurn)
	ctx := context.Background()
	if _, err := client.request(ctx, http.MethodGet, client.generationPath(), nil, false); err != nil {
		t.Fatal(err)
	}
	// The game stops accepting the token early, as after a key rotation.
	server.mutex.Lock()
	clear(server.valid)
	server.mutex.Unlock()
	if _, err := client.request(ctx, http.MethodGet, client.generationPath(), nil, false); err != nil {
		t.Fatal(err)
	}
	if issued := server.tokensIssued(); issued != 2 {
		t.Fatalf("tokens issued = %d, want 2", issued)
	}
	// A new token that is refused too stops the client.
	server.mutex.Lock()
	server.refuseTokens = true
	server.mutex.Unlock()
	if _, err := client.request(ctx, http.MethodGet, client.generationPath(), nil, false); !errors.Is(err, ErrAuthentication) {
		t.Fatalf("error = %v, want ErrAuthentication", err)
	}
	if issued := server.tokensIssued(); issued != 3 {
		t.Fatalf("tokens issued = %d, want 3", issued)
	}
	if _, err := client.request(ctx, http.MethodGet, "/game/upgrades", nil, false); !errors.Is(err, ErrAuthentication) {
		t.Fatalf("after the refusal: error = %v, want ErrAuthentication", err)
	}
}

func TestASessionTheLobbyRefusesStopsTheClient(t *testing.T) {
	server := newFakeServer(t)
	client, err := NewClient(server.server.URL, "stale", testLobbyID, passTurn, WithLogOutput(nil))
	if err != nil {
		t.Fatal(err)
	}
	var failure *GameError
	if _, err := client.request(context.Background(), http.MethodGet, client.generationPath(), nil, false); !errors.As(err, &failure) || failure.Status != 401 {
		t.Fatalf("error = %v, want a 401", err)
	}
	if issued := server.tokensIssued(); issued != 0 {
		t.Fatalf("tokens issued = %d", issued)
	}
}
