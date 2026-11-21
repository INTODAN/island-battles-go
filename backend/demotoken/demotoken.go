// Package demotoken issues and verifies demo tokens: a read-only token that
// lets whoever holds it download one game's demo as one roster player sees
// it, and nothing else. A GameEndedDelivery carries one in its demo link, so
// that a bot that runs outside the backend, which has no player token, can
// keep the game it played (#413).
//
// A token is "ibdt1.<payload>.<mac>": the compact JSON payload
// {"game_id","player_id","exp"}, where exp is a Unix time in seconds, and its
// HMAC-SHA256, both base64url without padding. The MAC's key is derived from
// the submit token secret for demo tokens alone, so a submit token never
// verifies as a demo token, nor the other way round. The server keeps nothing
// per token.
package demotoken

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// Prefix starts every token, so that it is told apart from a player token
// and a submit token at a glance.
const Prefix = "ibdt1."

// keyLabel derives the demo tokens' key from the shared secret.
const keyLabel = "island-battles demo token v1"

// ErrInvalid is returned for a token that is malformed, signed with another
// secret, or expired. It says no more, on purpose.
var ErrInvalid = errors.New("the demo token is not valid")

// Claims are what a token allows: downloading game GameID's demo as PlayerID
// sees it, until Expires.
type Claims struct {
	GameID   uint
	PlayerID string
	Expires  time.Time
}

// payload is the claims as they are signed.
type payload struct {
	GameID   uint   `json:"game_id"`
	PlayerID string `json:"player_id"`
	Expires  int64  `json:"exp"`
}

// Signer issues and verifies tokens with one secret.
type Signer struct{ key []byte }

// New returns a Signer for secret, the submit token secret, which must not
// be empty.
func New(secret []byte) (*Signer, error) {
	if len(secret) == 0 {
		return nil, errors.New("the demo token secret is empty")
	}
	derive := hmac.New(sha256.New, secret)
	derive.Write([]byte(keyLabel))
	return &Signer{key: derive.Sum(nil)}, nil
}

// Issue returns the token for claims. Expires is kept to the second.
func (signer *Signer) Issue(claims Claims) (string, error) {
	data, err := json.Marshal(payload{GameID: claims.GameID, PlayerID: claims.PlayerID, Expires: claims.Expires.Unix()})
	if err != nil {
		return "", err
	}
	return Prefix + base64.RawURLEncoding.EncodeToString(data) + "." + base64.RawURLEncoding.EncodeToString(signer.mac(data)), nil
}

// Verify checks a token's signature, in constant time, and that it hasn't
// expired at now, and returns its claims. Every failure is ErrInvalid.
func (signer *Signer) Verify(token string, now time.Time) (Claims, error) {
	rest, found := strings.CutPrefix(token, Prefix)
	if !found {
		return Claims{}, ErrInvalid
	}
	encodedPayload, encodedMAC, found := strings.Cut(rest, ".")
	if !found {
		return Claims{}, ErrInvalid
	}
	data, err := base64.RawURLEncoding.DecodeString(encodedPayload)
	if err != nil {
		return Claims{}, ErrInvalid
	}
	mac, err := base64.RawURLEncoding.DecodeString(encodedMAC)
	if err != nil || !hmac.Equal(mac, signer.mac(data)) {
		return Claims{}, ErrInvalid
	}
	var decoded payload
	if err := json.Unmarshal(data, &decoded); err != nil {
		return Claims{}, ErrInvalid
	}
	claims := Claims{GameID: decoded.GameID, PlayerID: decoded.PlayerID, Expires: time.Unix(decoded.Expires, 0)}
	if claims.GameID == 0 || claims.PlayerID == "" || !now.Before(claims.Expires) {
		return Claims{}, ErrInvalid
	}
	return claims, nil
}

func (signer *Signer) mac(data []byte) []byte {
	hash := hmac.New(sha256.New, signer.key)
	hash.Write(data)
	return hash.Sum(nil)
}

// FromAuthorization returns the token of an Authorization header that
// carries one as its bearer credential, and false for any other header, such
// as a player token.
func FromAuthorization(authorization string) (string, bool) {
	scheme, credential, found := strings.Cut(strings.TrimSpace(authorization), " ")
	if !found || !strings.EqualFold(scheme, "Bearer") {
		return "", false
	}
	credential = strings.TrimSpace(credential)
	return credential, strings.HasPrefix(credential, Prefix)
}
