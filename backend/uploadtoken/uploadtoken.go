// Package uploadtoken issues and verifies upload tokens: a short-lived token
// that lets whoever holds it upload bot packages to one lobby as one player,
// and nothing else. The bot picker issues one for the command line, which
// doesn't hold the player's session: island-battles-bot upload and the Go
// SDK's island-battles-package upload send it in place of the session.
//
// A token is "ibut1.<payload>.<mac>": the compact JSON payload
// {"lobby_id","player_id","session_id","exp"}, where exp is a Unix time in
// seconds, and its HMAC-SHA256 under the secret, both base64url without
// padding. The MAC covers the prefix too, so a token of another kind signed
// with the same secret, such as a submit token, is never an upload token.
// The CLI reads lobby_id from the payload, which is not secret. The server
// keeps nothing per token.
package uploadtoken

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// Prefix starts every token, so that it is told apart from a player's
// session at a glance.
const Prefix = "ibut1."

// Lifetime is how long a token is valid.
const Lifetime = 15 * time.Minute

// ErrInvalid is returned for a token that is malformed, signed with another
// secret, or expired. It says no more, on purpose.
var ErrInvalid = errors.New("the upload token is not valid")

// Claims are what a token allows: uploading packages to lobby LobbyID as
// PlayerID, whose session SessionID issued it, until Expires.
type Claims struct {
	LobbyID   string
	PlayerID  string
	SessionID string
	Expires   time.Time
}

// payload is the claims as they are signed.
type payload struct {
	LobbyID   string `json:"lobby_id"`
	PlayerID  string `json:"player_id"`
	SessionID string `json:"session_id"`
	Expires   int64  `json:"exp"`
}

// Signer issues and verifies tokens with one secret.
type Signer struct{ secret []byte }

// New returns a Signer for secret, which must not be empty.
func New(secret []byte) (*Signer, error) {
	if len(secret) == 0 {
		return nil, errors.New("the upload token secret is empty")
	}
	return &Signer{secret: append([]byte(nil), secret...)}, nil
}

// Issue returns the token for claims. Expires is kept to the second.
func (signer *Signer) Issue(claims Claims) (string, error) {
	data, err := json.Marshal(payload{LobbyID: claims.LobbyID, PlayerID: claims.PlayerID, SessionID: claims.SessionID, Expires: claims.Expires.Unix()})
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
	claims := Claims{LobbyID: decoded.LobbyID, PlayerID: decoded.PlayerID, SessionID: decoded.SessionID, Expires: time.Unix(decoded.Expires, 0)}
	if claims.LobbyID == "" || claims.PlayerID == "" || claims.SessionID == "" || !now.Before(claims.Expires) {
		return Claims{}, ErrInvalid
	}
	return claims, nil
}

// mac signs the prefix and the payload.
func (signer *Signer) mac(data []byte) []byte {
	hash := hmac.New(sha256.New, signer.secret)
	hash.Write([]byte(Prefix))
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
