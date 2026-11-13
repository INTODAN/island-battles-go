// Package hostedbot checks the URL of a hosted bot: a player's own server,
// at a public URL, that speaks the turn delivery protocol. The game sends
// turns to that URL from the server, so it must name a public host and
// nothing on the server's own network.
//
// A URL must be https://, at most MaxURLLength characters, without
// credentials, on port 443 or none. Its host must be a name, not an IP
// literal, and every address the name resolves to must be public: not
// loopback, private, link-local, unique-local or unspecified. The name is
// resolved when the bot is chosen, with Check. Nothing is sent to the bot
// then.
//
// What a name resolves to can change after that (DNS rebinding), so whoever
// sends a turn checks again as it sends: Client connects only to public
// addresses on port 443, checking the very address it connects to, and
// follows no redirects. The native bot runner sends hosted bots' turns with
// it (backend/botrunner/native).
package hostedbot

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"syscall"
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

// DialControl refuses to connect to an address that isn't Public, or to a
// port other than 443. A net.Dialer calls it with each address it is about
// to connect to, after resolving the name, so the check covers what the
// connection really reaches, whatever the name resolved to before.
func DialControl(_, address string, _ syscall.RawConn) error {
	addressPort, err := netip.ParseAddrPort(address)
	if err != nil {
		return refuse("a hosted bot's connection must go to an IP address and port, not %q", address)
	}
	if addressPort.Port() != 443 {
		return refuse("a hosted bot's connection must use port 443, not %d", addressPort.Port())
	}
	if !Public(addressPort.Addr()) {
		return refuse("the bot's host resolves to %s, an address on a private network: a hosted bot needs a public address", addressPort.Addr().Unmap())
	}
	return nil
}

// Client is an HTTP client for sending a hosted bot its turns, whose URL
// passed CheckSyntax. It connects only where DialControl allows, uses no
// proxy, follows no redirects, and gives up after timeout.
func Client(timeout time.Duration) *http.Client {
	dialer := &net.Dialer{Timeout: timeout, Control: DialControl}
	transport := &http.Transport{
		Proxy:               nil,
		DialContext:         dialer.DialContext,
		TLSHandshakeTimeout: timeout,
		ForceAttemptHTTP2:   true,
		MaxIdleConns:        10,
		IdleConnTimeout:     30 * time.Second,
	}
	return &http.Client{Transport: transport, Timeout: timeout, CheckRedirect: refuseRedirects}
}

func refuseRedirects(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }

// Loopback reports whether raw is an http or https URL on this machine:
// localhost, 127.0.0.1 or [::1], on any port, without credentials. They break
// the rules above, so only local development accepts them, for a bot server
// such as island-battles-bot serve on the developer's own machine.
func Loopback(raw string) bool {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.User != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return false
	}
	switch strings.ToLower(parsed.Hostname()) {
	case "localhost", "127.0.0.1", "::1":
		return true
	}
	return false
}
