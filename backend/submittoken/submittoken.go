// Package submittoken issues and verifies submit tokens: a token that lets
// whoever holds it submit one player's turn of one generation of one game, and
// nothing else. A turn delivery carries one, so that a bot that runs outside
// the backend, such as the player's localhost bot, submits its own commands
// without the player's session.
//
// A token is "ibst1.<payload>.<mac>": the compact JSON payload
// {"game_id","player_id","generation_id","exp"}, where exp is a Unix time in
// seconds, and its HMAC-SHA256 under the secret, both base64url without
// padding. The server keeps nothing per token.
package submittoken

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// Prefix starts every token, so that a bearer credential is told apart from
// a player's session at a glance.
const Prefix = "ibst1."

// ErrInvalid is returned for a token that is malformed, signed with another
// secret, or expired. It says no more, on purpose.
var ErrInvalid = errors.New("the submit token is not valid")

// Claims are what a token allows: submitting PlayerID's turn of generation
// GenerationID of game GameID, until Expires.
type Claims struct {
	GameID       uint
	PlayerID     string
	GenerationID uint
	Expires      time.Time
}

// payload is the claims as they are signed.
type payload struct {
	GameID       uint   `json:"game_id"`
	PlayerID     string `json:"player_id"`
	GenerationID uint   `json:"generation_id"`
	Expires      int64  `json:"exp"`
}

// Signer issues and verifies tokens with one secret.
type Signer struct{ secret []byte }

// New returns a Signer for secret, which must not be empty.
func New(secret []byte) (*Signer, error) {
	if len(secret) == 0 {
		return nil, errors.New("the submit token secret is empty")
	}
	return &Signer{secret: append([]byte(nil), secret...)}, nil
}

// Issue returns the token for claims. Expires is kept to the second.
func (signer *Signer) Issue(claims Claims) (string, error) {
	data, err := json.Marshal(payload{GameID: claims.GameID, PlayerID: claims.PlayerID, GenerationID: claims.GenerationID, Expires: claims.Expires.Unix()})
	if err != nil {
		return "", err
	}
	encoded := base64.RawURLEncoding.EncodeToString(data)
	return Prefix + encoded + "." + base64.RawURLEncoding.EncodeToString(signer.mac(data)), nil
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
	claims := Claims{GameID: decoded.GameID, PlayerID: decoded.PlayerID, GenerationID: decoded.GenerationID, Expires: time.Unix(decoded.Expires, 0)}
	if claims.GameID == 0 || claims.GenerationID == 0 || claims.PlayerID == "" || !now.Before(claims.Expires) {
		return Claims{}, ErrInvalid
	}
	return claims, nil
}

func (signer *Signer) mac(data []byte) []byte {
	hash := hmac.New(sha256.New, signer.secret)
	hash.Write(data)
	return hash.Sum(nil)
}

// FromAuthorization returns the token of an Authorization header that
// carries one as its bearer credential, and false for any other header, such
// as a player's session.
func FromAuthorization(authorization string) (string, bool) {
	scheme, credential, found := strings.Cut(strings.TrimSpace(authorization), " ")
	if !found || !strings.EqualFold(scheme, "Bearer") {
		return "", false
	}
	credential = strings.TrimSpace(credential)
	return credential, strings.HasPrefix(credential, Prefix)
}
