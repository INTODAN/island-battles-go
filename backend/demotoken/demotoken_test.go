package demotoken_test

import (
	"strings"
	"testing"
	"time"

	"islandBattles/backend/demotoken"
	"islandBattles/backend/submittoken"
)

func TestIssueAndVerify(t *testing.T) {
	signer, err := demotoken.New([]byte("secret"))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1_800_000_000, 0)
	claims := demotoken.Claims{GameID: 7, PlayerID: "a1b2", Expires: now.Add(time.Hour)}
	token, err := signer.Issue(claims)
	if err != nil || !strings.HasPrefix(token, demotoken.Prefix) {
		t.Fatalf("issue: %q, %v", token, err)
	}
	verified, err := signer.Verify(token, now)
	if err != nil || verified != claims {
		t.Fatalf("verify: %+v, %v", verified, err)
	}
	if _, err := signer.Verify(token, now.Add(time.Hour)); err != demotoken.ErrInvalid {
		t.Fatalf("an expired token verified: %v", err)
	}
	other, _ := demotoken.New([]byte("other"))
	if _, err := other.Verify(token, now); err != demotoken.ErrInvalid {
		t.Fatalf("a token of another secret verified: %v", err)
	}
	if _, err := signer.Verify(token[:len(token)-2]+"AA", now); err != demotoken.ErrInvalid {
		t.Fatalf("an altered token verified: %v", err)
	}
	if got, ok := demotoken.FromAuthorization("Bearer " + token); !ok || got != token {
		t.Fatalf("FromAuthorization: %q, %v", got, ok)
	}
	if _, ok := demotoken.FromAuthorization("Bearer eyJhbGciOi"); ok {
		t.Fatal("a player token was taken for a demo token")
	}
}

// A submit token of the same secret, payload and all, never verifies as a
// demo token: the demo tokens' key is derived for them alone.
func TestSubmitTokenIsNotADemoToken(t *testing.T) {
	secret := []byte("secret")
	submit, _ := submittoken.New(secret)
	demo, _ := demotoken.New(secret)
	now := time.Unix(1_800_000_000, 0)
	token, err := submit.Issue(submittoken.Claims{GameID: 7, PlayerID: "a1b2", GenerationID: 3, Expires: now.Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := demo.Verify(demotoken.Prefix+strings.TrimPrefix(token, submittoken.Prefix), now); err != demotoken.ErrInvalid {
		t.Fatalf("a submit token verified as a demo token: %v", err)
	}
	demoToken, _ := demo.Issue(demotoken.Claims{GameID: 7, PlayerID: "a1b2", Expires: now.Add(time.Hour)})
	if _, err := submit.Verify(submittoken.Prefix+strings.TrimPrefix(demoToken, demotoken.Prefix), now); err != submittoken.ErrInvalid {
		t.Fatalf("a demo token verified as a submit token: %v", err)
	}
}
