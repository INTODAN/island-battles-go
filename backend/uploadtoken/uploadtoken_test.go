package uploadtoken

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"strings"
	"testing"
	"time"

	"islandBattles/backend/submittoken"
)

func TestRoundTrip(t *testing.T) {
	signer, err := New([]byte("secret"))
	if err != nil {
		t.Fatal(err)
	}
	expires := time.Unix(2_000_000_000, 0)
	claims := Claims{LobbyID: "lobby", PlayerID: "player", Expires: expires}
	token, err := signer.Issue(claims)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(token, "ibut1.") || strings.Count(token, ".") != 2 || strings.ContainsAny(token, "+/=") {
		t.Fatalf("token %q is not ibut1.<base64url>.<base64url>", token)
	}
	payload, _ := base64.RawURLEncoding.DecodeString(strings.Split(token, ".")[1])
	if string(payload) != `{"lobby_id":"lobby","player_id":"player","exp":2000000000}` {
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
	claims := Claims{LobbyID: "l", PlayerID: "p", Expires: now.Add(time.Minute)}
	token, _ := signer.Issue(claims)
	parts := strings.Split(token, ".")
	forged := base64.RawURLEncoding.EncodeToString([]byte(`{"lobby_id":"other","player_id":"p","exp":9999999999}`))
	otherToken, _ := other.Issue(claims)
	empty, _ := signer.Issue(Claims{LobbyID: "l", Expires: now.Add(time.Minute)})
	// A MAC over the payload alone, as a submit token's is, isn't an upload
	// token's.
	unprefixed := hmac.New(sha256.New, []byte("secret"))
	payload, _ := base64.RawURLEncoding.DecodeString(parts[1])
	unprefixed.Write(payload)
	for name, candidate := range map[string]string{
		"another secret":      otherToken,
		"changed payload":     parts[0] + "." + forged + "." + parts[2],
		"no prefix":           strings.TrimPrefix(token, Prefix),
		"no mac":              parts[0] + "." + parts[1],
		"bad base64":          parts[0] + ".!!." + parts[2],
		"truncated mac":       token[:len(token)-2],
		"empty player":        empty,
		"a session":           "session-credential",
		"empty":               "",
		"payload not JSON":    Prefix + base64.RawURLEncoding.EncodeToString([]byte("x")) + "." + parts[2],
		"an extra part":       token + ".x",
		"mac without prefix":  parts[0] + "." + parts[1] + "." + base64.RawURLEncoding.EncodeToString(unprefixed.Sum(nil)),
		"submit token prefix": "ibst1." + parts[1] + "." + parts[2],
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

func TestASubmitTokenWithTheSameSecretIsNotAnUploadToken(t *testing.T) {
	submit, _ := submittoken.New([]byte("secret"))
	upload, _ := New([]byte("secret"))
	now := time.Unix(1_000, 0)
	token, err := submit.Issue(submittoken.Claims{GameID: 1, PlayerID: "p", GenerationID: 2, Expires: now.Add(time.Minute)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := upload.Verify(token, now); !errors.Is(err, ErrInvalid) {
		t.Fatalf("a submit token verified as an upload token: %v", err)
	}
	if _, err := upload.Verify(Prefix+strings.TrimPrefix(token, submittoken.Prefix), now); !errors.Is(err, ErrInvalid) {
		t.Fatalf("a submit token with the upload prefix verified: %v", err)
	}
	uploadToken, _ := upload.Issue(Claims{LobbyID: "l", PlayerID: "p", Expires: now.Add(time.Minute)})
	if _, err := submit.Verify(submittoken.Prefix+strings.TrimPrefix(uploadToken, Prefix), now); !errors.Is(err, submittoken.ErrInvalid) {
		t.Fatalf("an upload token verified as a submit token: %v", err)
	}
}

func TestFromAuthorization(t *testing.T) {
	for header, want := range map[string]string{"Bearer ibut1.a.b": "ibut1.a.b", "bearer  ibut1.a.b ": "ibut1.a.b"} {
		if token, ok := FromAuthorization(header); !ok || token != want {
			t.Errorf("%q: got %q %v", header, token, ok)
		}
	}
	for _, header := range []string{"Bearer session", "", "ibut1.a.b", "Basic ibut1.a.b", "Bearer ibst1.a.b"} {
		if _, ok := FromAuthorization(header); ok {
			t.Errorf("%q was taken for an upload token", header)
		}
	}
}
