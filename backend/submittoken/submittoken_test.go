package submittoken

import (
	"encoding/base64"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestRoundTrip(t *testing.T) {
	signer, err := New([]byte("secret"))
	if err != nil {
		t.Fatal(err)
	}
	expires := time.Unix(2_000_000_000, 0)
	claims := Claims{GameID: 7, PlayerID: "player", GenerationID: 12, Expires: expires}
	token, err := signer.Issue(claims)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(token, "ibst1.") || strings.Count(token, ".") != 2 || strings.ContainsAny(token, "+/=") {
		t.Fatalf("token %q is not ibst1.<base64url>.<base64url>", token)
	}
	payload, _ := base64.RawURLEncoding.DecodeString(strings.Split(token, ".")[1])
	if string(payload) != `{"game_id":7,"player_id":"player","generation_id":12,"exp":2000000000}` {
		t.Fatalf("payload %s", payload)
	}
	verified, err := signer.Verify(token, expires.Add(-time.Second))
	if err != nil || verified != claims {
		t.Fatalf("verified %+v, %v", verified, err)
	}
}

func TestRefusals(t *testing.T) {
	signer, _ := New([]byte("secret"))
	other, _ := New([]byte("another secret"))
	now := time.Unix(1_000, 0)
	token, _ := signer.Issue(Claims{GameID: 1, PlayerID: "p", GenerationID: 2, Expires: now.Add(time.Minute)})
	parts := strings.Split(token, ".")
	forged := base64.RawURLEncoding.EncodeToString([]byte(`{"game_id":2,"player_id":"p","generation_id":2,"exp":9999999999}`))
	otherToken, _ := other.Issue(Claims{GameID: 1, PlayerID: "p", GenerationID: 2, Expires: now.Add(time.Minute)})
	empty, _ := signer.Issue(Claims{GameID: 1, GenerationID: 2, Expires: now.Add(time.Minute)})
	for name, candidate := range map[string]string{
		"another secret":   otherToken,
		"changed payload":  parts[0] + "." + forged + "." + parts[2],
		"no prefix":        strings.TrimPrefix(token, Prefix),
		"no mac":           parts[0] + "." + parts[1],
		"bad base64":       parts[0] + ".!!." + parts[2],
		"truncated mac":    token[:len(token)-2],
		"empty player":     empty,
		"a session":        "session-credential",
		"empty":            "",
		"payload not JSON": Prefix + base64.RawURLEncoding.EncodeToString([]byte("x")) + "." + parts[2],
		"an extra part":    token + ".x",
	} {
		if _, err := signer.Verify(candidate, now); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: got %v", name, err)
		}
	}
	if _, err := signer.Verify(token, now.Add(time.Minute)); !errors.Is(err, ErrInvalid) {
		t.Errorf("an expired token got %v", err)
	}
	if _, err := New(nil); err == nil {
		t.Error("an empty secret was accepted")
	}
}

func TestFromAuthorization(t *testing.T) {
	for header, want := range map[string]string{"Bearer ibst1.a.b": "ibst1.a.b", "bearer  ibst1.a.b ": "ibst1.a.b"} {
		if token, ok := FromAuthorization(header); !ok || token != want {
			t.Errorf("%q: got %q %v", header, token, ok)
		}
	}
	for _, header := range []string{"Bearer session", "", "ibst1.a.b", "Basic ibst1.a.b"} {
		if _, ok := FromAuthorization(header); ok {
			t.Errorf("%q was taken for a submit token", header)
		}
	}
}
