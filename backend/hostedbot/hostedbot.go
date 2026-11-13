// Package hostedbot checks the URL of a hosted bot: a player's own server,
// at a public URL, that speaks the turn delivery protocol. The game sends
// turns to that URL from the server, so it must name a public host and
// nothing on the server's own network.
//
// A URL must be https://, at most MaxURLLength characters, without
// credentials, on port 443 or none. Its host must be a name, not an IP
// literal, and every address the name resolves to must be public: not
// loopback, private, link-local, unique-local or unspecified. The name is
// resolved when the bot is chosen, and again before each turn is sent, since
// what a name resolves to can change. Nothing is sent to the bot here.
package hostedbot

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"
)

// MaxURLLength is the longest URL a hosted bot may have, in characters.
const MaxURLLength = 2048

// LookupTimeout bounds the lookup of a hosted bot's host.
const LookupTimeout = 3 * time.Second

// Resolver looks up the addresses of a host name. *net.Resolver is one.
type Resolver interface {
	LookupNetIP(ctx context.Context, network, host string) ([]netip.Addr, error)
}

// DefaultResolver is the system's resolver.
var DefaultResolver Resolver = net.DefaultResolver

// Invalid is a URL that breaks a rule. Its message says which, for the
// player who chose it.
type Invalid struct{ Reason string }

func (invalid *Invalid) Error() string { return invalid.Reason }

func refuse(format string, arguments ...any) error {
	return &Invalid{Reason: fmt.Sprintf(format, arguments...)}
}

// Check checks a hosted bot's URL, resolving its host with resolver (nil is
// DefaultResolver), and returns the URL's host name. A URL that breaks a rule
// is an *Invalid.
func Check(ctx context.Context, resolver Resolver, raw string) (string, error) {
	host, err := CheckSyntax(raw)
	if err != nil {
		return "", err
	}
	if resolver == nil {
		resolver = DefaultResolver
	}
	lookup, cancel := context.WithTimeout(ctx, LookupTimeout)
	defer cancel()
	addresses, err := resolver.LookupNetIP(lookup, "ip", host)
	if err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		return "", refuse("the bot's host %s could not be looked up: check the name, or try again", host)
	}
	if len(addresses) == 0 {
		return "", refuse("the bot's host %s has no address", host)
	}
	for _, address := range addresses {
		if !Public(address) {
			return "", refuse("the bot's host %s resolves to %s, an address on a private network: a hosted bot needs a public address", host, address.Unmap())
		}
	}
	return host, nil
}

// CheckSyntax checks every rule of a hosted bot's URL that needs no lookup,
// and returns its host name.
func CheckSyntax(raw string) (string, error) {
	if utf8.RuneCountInString(raw) > MaxURLLength {
		return "", refuse("the bot's URL is longer than %d characters", MaxURLLength)
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Opaque != "" {
		return "", refuse("the bot's URL is not a URL, such as https://bot.example.com/turn")
	}
	if parsed.Scheme != "https" {
		return "", refuse("the bot's URL must start with https://")
	}
	if parsed.User != nil {
		return "", refuse("the bot's URL must not hold a user name or password")
	}
	host := parsed.Hostname()
	if host == "" {
		return "", refuse("the bot's URL names no host")
	}
	if port := parsed.Port(); port != "" && port != "443" {
		return "", refuse("the bot's URL must use port 443, or name no port")
	}
	if strings.HasSuffix(parsed.Host, ":") {
		return "", refuse("the bot's URL must use port 443, or name no port")
	}
	if _, err := netip.ParseAddr(host); err != nil && net.ParseIP(host) == nil {
		return strings.ToLower(host), nil
	}
	return "", refuse("the bot's URL must name its host, not an IP address")
}

// Public reports whether address is one the server may send a hosted bot's
// turns to: not loopback, private (which covers IPv6 unique-local),
// link-local, unspecified or multicast.
func Public(address netip.Addr) bool {
	address = address.Unmap()
	return address.IsValid() && !address.IsLoopback() && !address.IsPrivate() && !address.IsLinkLocalUnicast() &&
		!address.IsLinkLocalMulticast() && !address.IsInterfaceLocalMulticast() && !address.IsMulticast() && !address.IsUnspecified()
}

// IsInvalid reports whether err is a refused URL rather than a failed check.
func IsInvalid(err error) bool {
	var invalid *Invalid
	return errors.As(err, &invalid)
}
